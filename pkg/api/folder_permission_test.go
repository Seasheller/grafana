package api

import (
	"testing"

	"github.com/Seasheller/grafana/pkg/api/dtos"
	"github.com/Seasheller/grafana/pkg/bus"
	"github.com/Seasheller/grafana/pkg/components/simplejson"
	m "github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/services/dashboards"
	"github.com/Seasheller/grafana/pkg/services/guardian"

	. "github.com/smartystreets/goconvey/convey"
)

func TestFolderPermissionApiEndpoint(t *testing.T) {
	Convey("Folder permissions test", t, func() {
		Convey("Given folder not exists", func() {
			mock := &fakeFolderService{
				GetFolderByUIDError: m.ErrFolderNotFound,
			}

			origNewFolderService := dashboards.NewFolderService
			mockFolderService(mock)

			loggedInUserScenarioWithRole("When calling GET on", "GET", "/api/folders/uid/permissions", "/api/folders/:uid/permissions", m.ROLE_EDITOR, func(sc *scenarioContext) {
				callGetFolderPermissions(sc)
				So(sc.resp.Code, ShouldEqual, 404)
			})

			cmd := dtos.UpdateDashboardAclCommand{
				Items: []dtos.DashboardAclUpdateItem{
					{UserId: 1000, Permission: m.PERMISSION_ADMIN},
				},
			}

			updateFolderPermissionScenario("When calling POST on", "/api/folders/uid/permissions", "/api/folders/:uid/permissions", cmd, func(sc *scenarioContext) {
				callUpdateFolderPermissions(sc)
				So(sc.resp.Code, ShouldEqual, 404)
			})

			Reset(func() {
				dashboards.NewFolderService = origNewFolderService
			})
		})

		Convey("Given user has no admin permissions", func() {
			origNewGuardian := guardian.New
			guardian.MockDashboardGuardian(&guardian.FakeDashboardGuardian{CanAdminValue: false})

			mock := &fakeFolderService{
				GetFolderByUIDResult: &m.Folder{
					Id:    1,
					Uid:   "uid",
					Title: "Folder",
				},
			}

			origNewFolderService := dashboards.NewFolderService
			mockFolderService(mock)

			loggedInUserScenarioWithRole("When calling GET on", "GET", "/api/folders/uid/permissions", "/api/folders/:uid/permissions", m.ROLE_EDITOR, func(sc *scenarioContext) {
				callGetFolderPermissions(sc)
				So(sc.resp.Code, ShouldEqual, 403)
			})

			cmd := dtos.UpdateDashboardAclCommand{
				Items: []dtos.DashboardAclUpdateItem{
					{UserId: 1000, Permission: m.PERMISSION_ADMIN},
				},
			}

			updateFolderPermissionScenario("When calling POST on", "/api/folders/uid/permissions", "/api/folders/:uid/permissions", cmd, func(sc *scenarioContext) {
				callUpdateFolderPermissions(sc)
				So(sc.resp.Code, ShouldEqual, 403)
			})

			Reset(func() {
				guardian.New = origNewGuardian
				dashboards.NewFolderService = origNewFolderService
			})
		})

		Convey("Given user has admin permissions and permissions to update", func() {
			origNewGuardian := guardian.New
			guardian.MockDashboardGuardian(&guardian.FakeDashboardGuardian{
				CanAdminValue:                    true,
				CheckPermissionBeforeUpdateValue: true,
				GetAclValue: []*m.DashboardAclInfoDTO{
					{OrgId: 1, DashboardId: 1, UserId: 2, Permission: m.PERMISSION_VIEW},
					{OrgId: 1, DashboardId: 1, UserId: 3, Permission: m.PERMISSION_EDIT},
					{OrgId: 1, DashboardId: 1, UserId: 4, Permission: m.PERMISSION_ADMIN},
					{OrgId: 1, DashboardId: 1, TeamId: 1, Permission: m.PERMISSION_VIEW},
					{OrgId: 1, DashboardId: 1, TeamId: 2, Permission: m.PERMISSION_ADMIN},
				},
			})

			mock := &fakeFolderService{
				GetFolderByUIDResult: &m.Folder{
					Id:    1,
					Uid:   "uid",
					Title: "Folder",
				},
			}

			origNewFolderService := dashboards.NewFolderService
			mockFolderService(mock)

			loggedInUserScenarioWithRole("When calling GET on", "GET", "/api/folders/uid/permissions", "/api/folders/:uid/permissions", m.ROLE_ADMIN, func(sc *scenarioContext) {
				callGetFolderPermissions(sc)
				So(sc.resp.Code, ShouldEqual, 200)
				respJSON, err := simplejson.NewJson(sc.resp.Body.Bytes())
				So(err, ShouldBeNil)
				So(len(respJSON.MustArray()), ShouldEqual, 5)
				So(respJSON.GetIndex(0).Get("userId").MustInt(), ShouldEqual, 2)
				So(respJSON.GetIndex(0).Get("permission").MustInt(), ShouldEqual, m.PERMISSION_VIEW)
			})

			cmd := dtos.UpdateDashboardAclCommand{
				Items: []dtos.DashboardAclUpdateItem{
					{UserId: 1000, Permission: m.PERMISSION_ADMIN},
				},
			}

			updateFolderPermissionScenario("When calling POST on", "/api/folders/uid/permissions", "/api/folders/:uid/permissions", cmd, func(sc *scenarioContext) {
				callUpdateFolderPermissions(sc)
				So(sc.resp.Code, ShouldEqual, 200)
			})

			Reset(func() {
				guardian.New = origNewGuardian
				dashboards.NewFolderService = origNewFolderService
			})
		})

		Convey("When trying to update permissions with duplicate permissions", func() {
			origNewGuardian := guardian.New
			guardian.MockDashboardGuardian(&guardian.FakeDashboardGuardian{
				CanAdminValue:                    true,
				CheckPermissionBeforeUpdateValue: false,
				CheckPermissionBeforeUpdateError: guardian.ErrGuardianPermissionExists,
			})

			mock := &fakeFolderService{
				GetFolderByUIDResult: &m.Folder{
					Id:    1,
					Uid:   "uid",
					Title: "Folder",
				},
			}

			origNewFolderService := dashboards.NewFolderService
			mockFolderService(mock)

			cmd := dtos.UpdateDashboardAclCommand{
				Items: []dtos.DashboardAclUpdateItem{
					{UserId: 1000, Permission: m.PERMISSION_ADMIN},
				},
			}

			updateFolderPermissionScenario("When calling POST on", "/api/folders/uid/permissions", "/api/folders/:uid/permissions", cmd, func(sc *scenarioContext) {
				callUpdateFolderPermissions(sc)
				So(sc.resp.Code, ShouldEqual, 400)
			})

			Reset(func() {
				guardian.New = origNewGuardian
				dashboards.NewFolderService = origNewFolderService
			})
		})

		Convey("When trying to override inherited permissions with lower presedence", func() {
			origNewGuardian := guardian.New
			guardian.MockDashboardGuardian(&guardian.FakeDashboardGuardian{
				CanAdminValue:                    true,
				CheckPermissionBeforeUpdateValue: false,
				CheckPermissionBeforeUpdateError: guardian.ErrGuardianOverride},
			)

			mock := &fakeFolderService{
				GetFolderByUIDResult: &m.Folder{
					Id:    1,
					Uid:   "uid",
					Title: "Folder",
				},
			}

			origNewFolderService := dashboards.NewFolderService
			mockFolderService(mock)

			cmd := dtos.UpdateDashboardAclCommand{
				Items: []dtos.DashboardAclUpdateItem{
					{UserId: 1000, Permission: m.PERMISSION_ADMIN},
				},
			}

			updateFolderPermissionScenario("When calling POST on", "/api/folders/uid/permissions", "/api/folders/:uid/permissions", cmd, func(sc *scenarioContext) {
				callUpdateFolderPermissions(sc)
				So(sc.resp.Code, ShouldEqual, 400)
			})

			Reset(func() {
				guardian.New = origNewGuardian
				dashboards.NewFolderService = origNewFolderService
			})
		})
	})
}

func callGetFolderPermissions(sc *scenarioContext) {
	sc.handlerFunc = GetFolderPermissionList
	sc.fakeReqWithParams("GET", sc.url, map[string]string{}).exec()
}

func callUpdateFolderPermissions(sc *scenarioContext) {
	bus.AddHandler("test", func(cmd *m.UpdateDashboardAclCommand) error {
		return nil
	})

	sc.fakeReqWithParams("POST", sc.url, map[string]string{}).exec()
}

func updateFolderPermissionScenario(desc string, url string, routePattern string, cmd dtos.UpdateDashboardAclCommand, fn scenarioFunc) {
	Convey(desc+" "+url, func() {
		defer bus.ClearBusHandlers()

		sc := setupScenarioContext(url)

		sc.defaultHandler = Wrap(func(c *m.ReqContext) Response {
			sc.context = c
			sc.context.OrgId = TestOrgID
			sc.context.UserId = TestUserID

			return UpdateFolderPermissions(c, cmd)
		})

		sc.m.Post(routePattern, sc.defaultHandler)

		fn(sc)
	})
}
