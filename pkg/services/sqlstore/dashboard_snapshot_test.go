package sqlstore

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/Seasheller/grafana/pkg/components/simplejson"
	m "github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/setting"
)

func TestDashboardSnapshotDBAccess(t *testing.T) {

	Convey("Testing DashboardSnapshot data access", t, func() {
		InitTestDB(t)

		Convey("Given saved snapshot", func() {
			cmd := m.CreateDashboardSnapshotCommand{
				Key: "hej",
				Dashboard: simplejson.NewFromAny(map[string]interface{}{
					"hello": "mupp",
				}),
				UserId: 1000,
				OrgId:  1,
			}
			err := CreateDashboardSnapshot(&cmd)
			So(err, ShouldBeNil)

			Convey("Should be able to get snapshot by key", func() {
				query := m.GetDashboardSnapshotQuery{Key: "hej"}
				err = GetDashboardSnapshot(&query)
				So(err, ShouldBeNil)

				So(query.Result, ShouldNotBeNil)
				So(query.Result.Dashboard.Get("hello").MustString(), ShouldEqual, "mupp")
			})

			Convey("And the user has the admin role", func() {
				Convey("Should return all the snapshots", func() {
					query := m.GetDashboardSnapshotsQuery{
						OrgId:        1,
						SignedInUser: &m.SignedInUser{OrgRole: m.ROLE_ADMIN},
					}
					err := SearchDashboardSnapshots(&query)
					So(err, ShouldBeNil)

					So(query.Result, ShouldNotBeNil)
					So(len(query.Result), ShouldEqual, 1)
				})
			})

			Convey("And the user has the editor role and has created a snapshot", func() {
				Convey("Should return all the snapshots", func() {
					query := m.GetDashboardSnapshotsQuery{
						OrgId:        1,
						SignedInUser: &m.SignedInUser{OrgRole: m.ROLE_EDITOR, UserId: 1000},
					}
					err := SearchDashboardSnapshots(&query)
					So(err, ShouldBeNil)

					So(query.Result, ShouldNotBeNil)
					So(len(query.Result), ShouldEqual, 1)
				})
			})

			Convey("And the user has the editor role and has not created any snapshot", func() {
				Convey("Should not return any snapshots", func() {
					query := m.GetDashboardSnapshotsQuery{
						OrgId:        1,
						SignedInUser: &m.SignedInUser{OrgRole: m.ROLE_EDITOR, UserId: 2},
					}
					err := SearchDashboardSnapshots(&query)
					So(err, ShouldBeNil)

					So(query.Result, ShouldNotBeNil)
					So(len(query.Result), ShouldEqual, 0)
				})
			})

			Convey("And the user is anonymous", func() {
				cmd := m.CreateDashboardSnapshotCommand{
					Key:       "strangesnapshotwithuserid0",
					DeleteKey: "adeletekey",
					Dashboard: simplejson.NewFromAny(map[string]interface{}{
						"hello": "mupp",
					}),
					UserId: 0,
					OrgId:  1,
				}
				err := CreateDashboardSnapshot(&cmd)
				So(err, ShouldBeNil)

				Convey("Should not return any snapshots", func() {
					query := m.GetDashboardSnapshotsQuery{
						OrgId:        1,
						SignedInUser: &m.SignedInUser{OrgRole: m.ROLE_EDITOR, IsAnonymous: true, UserId: 0},
					}
					err := SearchDashboardSnapshots(&query)
					So(err, ShouldBeNil)

					So(query.Result, ShouldNotBeNil)
					So(len(query.Result), ShouldEqual, 0)
				})
			})
		})
	})
}

func TestDeleteExpiredSnapshots(t *testing.T) {
	sqlstore := InitTestDB(t)

	Convey("Testing dashboard snapshots clean up", t, func() {
		setting.SnapShotRemoveExpired = true

		notExpiredsnapshot := createTestSnapshot(sqlstore, "key1", 48000)
		createTestSnapshot(sqlstore, "key2", -1200)
		createTestSnapshot(sqlstore, "key3", -1200)

		err := DeleteExpiredSnapshots(&m.DeleteExpiredSnapshotsCommand{})
		So(err, ShouldBeNil)

		query := m.GetDashboardSnapshotsQuery{
			OrgId:        1,
			SignedInUser: &m.SignedInUser{OrgRole: m.ROLE_ADMIN},
		}
		err = SearchDashboardSnapshots(&query)
		So(err, ShouldBeNil)

		So(len(query.Result), ShouldEqual, 1)
		So(query.Result[0].Key, ShouldEqual, notExpiredsnapshot.Key)

		err = DeleteExpiredSnapshots(&m.DeleteExpiredSnapshotsCommand{})
		So(err, ShouldBeNil)

		query = m.GetDashboardSnapshotsQuery{
			OrgId:        1,
			SignedInUser: &m.SignedInUser{OrgRole: m.ROLE_ADMIN},
		}
		SearchDashboardSnapshots(&query)

		So(len(query.Result), ShouldEqual, 1)
		So(query.Result[0].Key, ShouldEqual, notExpiredsnapshot.Key)
	})
}

func createTestSnapshot(sqlstore *SqlStore, key string, expires int64) *m.DashboardSnapshot {
	cmd := m.CreateDashboardSnapshotCommand{
		Key:       key,
		DeleteKey: "delete" + key,
		Dashboard: simplejson.NewFromAny(map[string]interface{}{
			"hello": "mupp",
		}),
		UserId:  1000,
		OrgId:   1,
		Expires: expires,
	}
	err := CreateDashboardSnapshot(&cmd)
	So(err, ShouldBeNil)

	// Set expiry date manually - to be able to create expired snapshots
	if expires < 0 {
		expireDate := time.Now().Add(time.Second * time.Duration(expires))
		_, err = sqlstore.engine.Exec("UPDATE dashboard_snapshot SET expires = ? WHERE id = ?", expireDate, cmd.Result.Id)
		So(err, ShouldBeNil)
	}

	return cmd.Result
}
