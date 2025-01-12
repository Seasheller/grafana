package dashboards

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Seasheller/grafana/pkg/services/dashboards"
	"github.com/Seasheller/grafana/pkg/util"

	"github.com/Seasheller/grafana/pkg/bus"

	"github.com/Seasheller/grafana/pkg/components/simplejson"
	"github.com/Seasheller/grafana/pkg/infra/log"
	"github.com/Seasheller/grafana/pkg/models"
)

var (
	ErrFolderNameMissing = errors.New("Folder name missing")
)

type fileReader struct {
	Cfg                          *DashboardsAsConfig
	Path                         string
	log                          log.Logger
	dashboardProvisioningService dashboards.DashboardProvisioningService
}

func NewDashboardFileReader(cfg *DashboardsAsConfig, log log.Logger) (*fileReader, error) {
	var path string
	path, ok := cfg.Options["path"].(string)
	if !ok {
		path, ok = cfg.Options["folder"].(string)
		if !ok {
			return nil, fmt.Errorf("Failed to load dashboards. path param is not a string")
		}

		log.Warn("[Deprecated] The folder property is deprecated. Please use path instead.")
	}

	return &fileReader{
		Cfg:                          cfg,
		Path:                         path,
		log:                          log,
		dashboardProvisioningService: dashboards.NewProvisioningService(),
	}, nil
}

// pollChanges periodically runs startWalkingDisk based on interval specified in the config.
func (fr *fileReader) pollChanges(ctx context.Context) {

	// TODO: Fix the staticcheck error
	ticker := time.Tick(time.Duration(int64(time.Second) * fr.Cfg.UpdateIntervalSeconds)) //nolint:staticcheck
	for {
		select {
		case <-ticker:
			if err := fr.startWalkingDisk(); err != nil {
				fr.log.Error("failed to search for dashboards", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// startWalkingDisk traverses the file system for defined path, reads dashboard definition files and applies any change
// to the database.
func (fr *fileReader) startWalkingDisk() error {
	fr.log.Debug("Start walking disk", "path", fr.Path)
	resolvedPath := fr.resolvedPath()
	if _, err := os.Stat(resolvedPath); err != nil {
		return err
	}

	folderId, err := getOrCreateFolderId(fr.Cfg, fr.dashboardProvisioningService)
	if err != nil && err != ErrFolderNameMissing {
		return err
	}

	provisionedDashboardRefs, err := getProvisionedDashboardByPath(fr.dashboardProvisioningService, fr.Cfg.Name)
	if err != nil {
		return err
	}

	filesFoundOnDisk := map[string]os.FileInfo{}
	err = filepath.Walk(resolvedPath, createWalkFn(filesFoundOnDisk))
	if err != nil {
		return err
	}

	fr.handleMissingDashboardFiles(provisionedDashboardRefs, filesFoundOnDisk)

	sanityChecker := newProvisioningSanityChecker(fr.Cfg.Name)

	// save dashboards based on json files
	for path, fileInfo := range filesFoundOnDisk {
		provisioningMetadata, err := fr.saveDashboard(path, folderId, fileInfo, provisionedDashboardRefs)
		sanityChecker.track(provisioningMetadata)
		if err != nil {
			fr.log.Error("failed to save dashboard", "error", err)
		}
	}
	sanityChecker.logWarnings(fr.log)

	return nil
}

// handleMissingDashboardFiles will unprovision or delete dashboards which are missing on disk.
func (fr *fileReader) handleMissingDashboardFiles(provisionedDashboardRefs map[string]*models.DashboardProvisioning, filesFoundOnDisk map[string]os.FileInfo) {
	// find dashboards to delete since json file is missing
	var dashboardToDelete []int64
	for path, provisioningData := range provisionedDashboardRefs {
		_, existsOnDisk := filesFoundOnDisk[path]
		if !existsOnDisk {
			dashboardToDelete = append(dashboardToDelete, provisioningData.DashboardId)
		}
	}

	if fr.Cfg.DisableDeletion {
		// If deletion is disabled for the provisioner we just remove provisioning metadata about the dashboard
		// so afterwards the dashboard is considered unprovisioned.
		for _, dashboardId := range dashboardToDelete {
			fr.log.Debug("unprovisioning provisioned dashboard. missing on disk", "id", dashboardId)
			err := fr.dashboardProvisioningService.UnprovisionDashboard(dashboardId)
			if err != nil {
				fr.log.Error("failed to unprovision dashboard", "dashboard_id", dashboardId, "error", err)
			}
		}
	} else {
		// delete dashboard that are missing json file
		for _, dashboardId := range dashboardToDelete {
			fr.log.Debug("deleting provisioned dashboard. missing on disk", "id", dashboardId)
			err := fr.dashboardProvisioningService.DeleteProvisionedDashboard(dashboardId, fr.Cfg.OrgId)
			if err != nil {
				fr.log.Error("failed to delete dashboard", "id", dashboardId, "error", err)
			}
		}
	}
}

// saveDashboard saves or updates the dashboard provisioning file at path.
func (fr *fileReader) saveDashboard(path string, folderId int64, fileInfo os.FileInfo, provisionedDashboardRefs map[string]*models.DashboardProvisioning) (provisioningMetadata, error) {
	provisioningMetadata := provisioningMetadata{}
	resolvedFileInfo, err := resolveSymlink(fileInfo, path)
	if err != nil {
		return provisioningMetadata, err
	}

	provisionedData, alreadyProvisioned := provisionedDashboardRefs[path]
	upToDate := alreadyProvisioned && provisionedData.Updated >= resolvedFileInfo.ModTime().Unix()

	jsonFile, err := fr.readDashboardFromFile(path, resolvedFileInfo.ModTime(), folderId)
	if err != nil {
		fr.log.Error("failed to load dashboard from ", "file", path, "error", err)
		return provisioningMetadata, nil
	}

	if provisionedData != nil && jsonFile.checkSum == provisionedData.CheckSum {
		upToDate = true
	}

	// keeps track of what uid's and title's we have already provisioned
	dash := jsonFile.dashboard
	provisioningMetadata.uid = dash.Dashboard.Uid
	provisioningMetadata.title = dash.Dashboard.Title

	if upToDate {
		return provisioningMetadata, nil
	}

	if dash.Dashboard.Id != 0 {
		dash.Dashboard.Data.Set("id", nil)
		dash.Dashboard.Id = 0
	}

	if alreadyProvisioned {
		dash.Dashboard.SetId(provisionedData.DashboardId)
	}

	fr.log.Debug("saving new dashboard", "provisioner", fr.Cfg.Name, "file", path, "folderId", dash.Dashboard.FolderId)
	dp := &models.DashboardProvisioning{
		ExternalId: path,
		Name:       fr.Cfg.Name,
		Updated:    resolvedFileInfo.ModTime().Unix(),
		CheckSum:   jsonFile.checkSum,
	}

	_, err = fr.dashboardProvisioningService.SaveProvisionedDashboard(dash, dp)
	return provisioningMetadata, err
}

func getProvisionedDashboardByPath(service dashboards.DashboardProvisioningService, name string) (map[string]*models.DashboardProvisioning, error) {
	arr, err := service.GetProvisionedDashboardData(name)
	if err != nil {
		return nil, err
	}

	byPath := map[string]*models.DashboardProvisioning{}
	for _, pd := range arr {
		byPath[pd.ExternalId] = pd
	}

	return byPath, nil
}

func getOrCreateFolderId(cfg *DashboardsAsConfig, service dashboards.DashboardProvisioningService) (int64, error) {
	if cfg.Folder == "" {
		return 0, ErrFolderNameMissing
	}

	cmd := &models.GetDashboardQuery{Slug: models.SlugifyTitle(cfg.Folder), OrgId: cfg.OrgId}
	err := bus.Dispatch(cmd)

	if err != nil && err != models.ErrDashboardNotFound {
		return 0, err
	}

	// dashboard folder not found. create one.
	if err == models.ErrDashboardNotFound {
		dash := &dashboards.SaveDashboardDTO{}
		dash.Dashboard = models.NewDashboardFolder(cfg.Folder)
		dash.Dashboard.IsFolder = true
		dash.Overwrite = true
		dash.OrgId = cfg.OrgId
		// set dashboard folderUid if given
		dash.Dashboard.SetUid(cfg.FolderUid)
		dbDash, err := service.SaveFolderForProvisionedDashboards(dash)
		if err != nil {
			return 0, err
		}

		return dbDash.Id, nil
	}

	if !cmd.Result.IsFolder {
		return 0, fmt.Errorf("got invalid response. expected folder, found dashboard")
	}

	return cmd.Result.Id, nil
}

func resolveSymlink(fileinfo os.FileInfo, path string) (os.FileInfo, error) {
	checkFilepath, err := filepath.EvalSymlinks(path)
	if path != checkFilepath {
		fi, err := os.Lstat(checkFilepath)
		if err != nil {
			return nil, err
		}

		return fi, nil
	}

	return fileinfo, err
}

func createWalkFn(filesOnDisk map[string]os.FileInfo) filepath.WalkFunc {
	return func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		isValid, err := validateWalkablePath(fileInfo)
		if !isValid {
			return err
		}

		filesOnDisk[path] = fileInfo
		return nil
	}
}

func validateWalkablePath(fileInfo os.FileInfo) (bool, error) {
	if fileInfo.IsDir() {
		if strings.HasPrefix(fileInfo.Name(), ".") {
			return false, filepath.SkipDir
		}
		return false, nil
	}

	if !strings.HasSuffix(fileInfo.Name(), ".json") {
		return false, nil
	}

	return true, nil
}

type dashboardJsonFile struct {
	dashboard    *dashboards.SaveDashboardDTO
	checkSum     string
	lastModified time.Time
}

func (fr *fileReader) readDashboardFromFile(path string, lastModified time.Time, folderId int64) (*dashboardJsonFile, error) {
	reader, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	all, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	checkSum, err := util.Md5SumString(string(all))
	if err != nil {
		return nil, err
	}

	data, err := simplejson.NewJson(all)
	if err != nil {
		return nil, err
	}

	dash, err := createDashboardJson(data, lastModified, fr.Cfg, folderId)
	if err != nil {
		return nil, err
	}

	return &dashboardJsonFile{
		dashboard:    dash,
		checkSum:     checkSum,
		lastModified: lastModified,
	}, nil
}

func (fr *fileReader) resolvedPath() string {
	if _, err := os.Stat(fr.Path); os.IsNotExist(err) {
		fr.log.Error("Cannot read directory", "error", err)
	}

	path, err := filepath.Abs(fr.Path)
	if err != nil {
		fr.log.Error("Could not create absolute path", "path", fr.Path, "error", err)
	}

	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		fr.log.Error("Failed to read content of symlinked path", "path", fr.Path, "error", err)
	}

	if path == "" {
		path = fr.Path
		fr.log.Info("falling back to original path due to EvalSymlink/Abs failure")
	}
	return path
}

type provisioningMetadata struct {
	uid   string
	title string
}

func newProvisioningSanityChecker(provisioningProvider string) provisioningSanityChecker {
	return provisioningSanityChecker{
		provisioningProvider: provisioningProvider,
		uidUsage:             map[string]uint8{},
		titleUsage:           map[string]uint8{}}
}

type provisioningSanityChecker struct {
	provisioningProvider string
	uidUsage             map[string]uint8
	titleUsage           map[string]uint8
}

func (checker provisioningSanityChecker) track(pm provisioningMetadata) {
	if len(pm.uid) > 0 {
		checker.uidUsage[pm.uid] += 1
	}
	if len(pm.title) > 0 {
		checker.titleUsage[pm.title] += 1
	}
}

func (checker provisioningSanityChecker) logWarnings(log log.Logger) {
	for uid, times := range checker.uidUsage {
		if times > 1 {
			log.Error("the same 'uid' is used more than once", "uid", uid, "provider", checker.provisioningProvider)
		}
	}

	for title, times := range checker.titleUsage {
		if times > 1 {
			log.Error("the same 'title' is used more than once", "title", title, "provider", checker.provisioningProvider)
		}
	}
}
