package usagestats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/Seasheller/grafana/pkg/infra/metrics"
	"github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/plugins"
	"github.com/Seasheller/grafana/pkg/setting"
)

var usageStatsURL = "https://stats.grafana.org/grafana-usage-report"

func (uss *UsageStatsService) sendUsageStats(oauthProviders map[string]bool) {
	if !setting.ReportingEnabled {
		return
	}

	metricsLogger.Debug(fmt.Sprintf("Sending anonymous usage stats to %s", usageStatsURL))

	version := strings.Replace(setting.BuildVersion, ".", "_", -1)

	metrics := map[string]interface{}{}
	report := map[string]interface{}{
		"version":   version,
		"metrics":   metrics,
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
		"edition":   getEdition(),
		"packaging": setting.Packaging,
	}

	statsQuery := models.GetSystemStatsQuery{}
	if err := uss.Bus.Dispatch(&statsQuery); err != nil {
		metricsLogger.Error("Failed to get system stats", "error", err)
		return
	}

	metrics["stats.dashboards.count"] = statsQuery.Result.Dashboards
	metrics["stats.users.count"] = statsQuery.Result.Users
	metrics["stats.orgs.count"] = statsQuery.Result.Orgs
	metrics["stats.playlist.count"] = statsQuery.Result.Playlists
	metrics["stats.plugins.apps.count"] = len(plugins.Apps)
	metrics["stats.plugins.panels.count"] = len(plugins.Panels)
	metrics["stats.plugins.datasources.count"] = len(plugins.DataSources)
	metrics["stats.alerts.count"] = statsQuery.Result.Alerts
	metrics["stats.active_users.count"] = statsQuery.Result.ActiveUsers
	metrics["stats.datasources.count"] = statsQuery.Result.Datasources
	metrics["stats.stars.count"] = statsQuery.Result.Stars
	metrics["stats.folders.count"] = statsQuery.Result.Folders
	metrics["stats.dashboard_permissions.count"] = statsQuery.Result.DashboardPermissions
	metrics["stats.folder_permissions.count"] = statsQuery.Result.FolderPermissions
	metrics["stats.provisioned_dashboards.count"] = statsQuery.Result.ProvisionedDashboards
	metrics["stats.snapshots.count"] = statsQuery.Result.Snapshots
	metrics["stats.teams.count"] = statsQuery.Result.Teams
	metrics["stats.total_auth_token.count"] = statsQuery.Result.AuthTokens

	userCount := statsQuery.Result.Users
	avgAuthTokensPerUser := statsQuery.Result.AuthTokens
	if userCount != 0 {
		avgAuthTokensPerUser = avgAuthTokensPerUser / userCount
	}

	metrics["stats.avg_auth_token_per_user.count"] = avgAuthTokensPerUser

	dsStats := models.GetDataSourceStatsQuery{}
	if err := uss.Bus.Dispatch(&dsStats); err != nil {
		metricsLogger.Error("Failed to get datasource stats", "error", err)
		return
	}

	// send counters for each data source
	// but ignore any custom data sources
	// as sending that name could be sensitive information
	dsOtherCount := 0
	for _, dsStat := range dsStats.Result {
		if models.IsKnownDataSourcePlugin(dsStat.Type) {
			metrics["stats.ds."+dsStat.Type+".count"] = dsStat.Count
		} else {
			dsOtherCount += dsStat.Count
		}
	}
	metrics["stats.ds.other.count"] = dsOtherCount

	metrics["stats.packaging."+setting.Packaging+".count"] = 1

	dsAccessStats := models.GetDataSourceAccessStatsQuery{}
	if err := uss.Bus.Dispatch(&dsAccessStats); err != nil {
		metricsLogger.Error("Failed to get datasource access stats", "error", err)
		return
	}

	// send access counters for each data source
	// but ignore any custom data sources
	// as sending that name could be sensitive information
	dsAccessOtherCount := make(map[string]int64)
	for _, dsAccessStat := range dsAccessStats.Result {
		if dsAccessStat.Access == "" {
			continue
		}

		access := strings.ToLower(dsAccessStat.Access)

		if models.IsKnownDataSourcePlugin(dsAccessStat.Type) {
			metrics["stats.ds_access."+dsAccessStat.Type+"."+access+".count"] = dsAccessStat.Count
		} else {
			old := dsAccessOtherCount[access]
			dsAccessOtherCount[access] = old + dsAccessStat.Count
		}
	}

	for access, count := range dsAccessOtherCount {
		metrics["stats.ds_access.other."+access+".count"] = count
	}

	anStats := models.GetAlertNotifierUsageStatsQuery{}
	if err := uss.Bus.Dispatch(&anStats); err != nil {
		metricsLogger.Error("Failed to get alert notification stats", "error", err)
		return
	}

	for _, stats := range anStats.Result {
		metrics["stats.alert_notifiers."+stats.Type+".count"] = stats.Count
	}

	authTypes := map[string]bool{}
	authTypes["anonymous"] = setting.AnonymousEnabled
	authTypes["basic_auth"] = setting.BasicAuthEnabled
	authTypes["ldap"] = setting.LDAPEnabled
	authTypes["auth_proxy"] = setting.AuthProxyEnabled

	for provider, enabled := range oauthProviders {
		authTypes["oauth_"+provider] = enabled
	}

	for authType, enabled := range authTypes {
		enabledValue := 0
		if enabled {
			enabledValue = 1
		}
		metrics["stats.auth_enabled."+authType+".count"] = enabledValue
	}

	out, _ := json.MarshalIndent(report, "", " ")
	data := bytes.NewBuffer(out)

	client := http.Client{Timeout: 5 * time.Second}
	go client.Post(usageStatsURL, "application/json", data)
}

func (uss *UsageStatsService) updateTotalStats() {
	statsQuery := models.GetSystemStatsQuery{}
	if err := uss.Bus.Dispatch(&statsQuery); err != nil {
		metricsLogger.Error("Failed to get system stats", "error", err)
		return
	}

	metrics.MStatTotalDashboards.Set(float64(statsQuery.Result.Dashboards))
	metrics.MStatTotalUsers.Set(float64(statsQuery.Result.Users))
	metrics.MStatActiveUsers.Set(float64(statsQuery.Result.ActiveUsers))
	metrics.MStatTotalPlaylists.Set(float64(statsQuery.Result.Playlists))
	metrics.MStatTotalOrgs.Set(float64(statsQuery.Result.Orgs))
	metrics.StatsTotalViewers.Set(float64(statsQuery.Result.Viewers))
	metrics.StatsTotalActiveViewers.Set(float64(statsQuery.Result.ActiveViewers))
	metrics.StatsTotalEditors.Set(float64(statsQuery.Result.Editors))
	metrics.StatsTotalActiveEditors.Set(float64(statsQuery.Result.ActiveEditors))
	metrics.StatsTotalAdmins.Set(float64(statsQuery.Result.Admins))
	metrics.StatsTotalActiveAdmins.Set(float64(statsQuery.Result.ActiveAdmins))
}

func getEdition() string {
	if setting.IsEnterprise {
		return "enterprise"
	}
	return "oss"
}
