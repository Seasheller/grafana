package sqlstore

import (
	"time"

	"github.com/Seasheller/grafana/pkg/bus"
	"github.com/Seasheller/grafana/pkg/events"
	m "github.com/Seasheller/grafana/pkg/models"
)

func init() {
	bus.AddHandler("sql", GetOrgById)
	bus.AddHandler("sql", CreateOrg)
	bus.AddHandler("sql", UpdateOrg)
	bus.AddHandler("sql", UpdateOrgAddress)
	bus.AddHandler("sql", GetOrgByName)
	bus.AddHandler("sql", SearchOrgs)
	bus.AddHandler("sql", DeleteOrg)
}

func SearchOrgs(query *m.SearchOrgsQuery) error {
	query.Result = make([]*m.OrgDTO, 0)
	sess := x.Table("org")
	if query.Query != "" {
		sess.Where("name LIKE ?", query.Query+"%")
	}
	if query.Name != "" {
		sess.Where("name=?", query.Name)
	}
	sess.Limit(query.Limit, query.Limit*query.Page)
	sess.Cols("id", "name")
	err := sess.Find(&query.Result)
	return err
}

func GetOrgById(query *m.GetOrgByIdQuery) error {
	var org m.Org
	exists, err := x.Id(query.Id).Get(&org)
	if err != nil {
		return err
	}

	if !exists {
		return m.ErrOrgNotFound
	}

	query.Result = &org
	return nil
}

func GetOrgByName(query *m.GetOrgByNameQuery) error {
	var org m.Org
	exists, err := x.Where("name=?", query.Name).Get(&org)
	if err != nil {
		return err
	}

	if !exists {
		return m.ErrOrgNotFound
	}

	query.Result = &org
	return nil
}

func isOrgNameTaken(name string, existingId int64, sess *DBSession) (bool, error) {
	// check if org name is taken
	var org m.Org
	exists, err := sess.Where("name=?", name).Get(&org)

	if err != nil {
		return false, nil
	}

	if exists && existingId != org.Id {
		return true, nil
	}

	return false, nil
}

func CreateOrg(cmd *m.CreateOrgCommand) error {
	return inTransaction(func(sess *DBSession) error {

		if isNameTaken, err := isOrgNameTaken(cmd.Name, 0, sess); err != nil {
			return err
		} else if isNameTaken {
			return m.ErrOrgNameTaken
		}

		org := m.Org{
			Name:    cmd.Name,
			Created: time.Now(),
			Updated: time.Now(),
		}

		if _, err := sess.Insert(&org); err != nil {
			return err
		}

		user := m.OrgUser{
			OrgId:   org.Id,
			UserId:  cmd.UserId,
			Role:    m.ROLE_ADMIN,
			Created: time.Now(),
			Updated: time.Now(),
		}

		_, err := sess.Insert(&user)
		cmd.Result = org

		sess.publishAfterCommit(&events.OrgCreated{
			Timestamp: org.Created,
			Id:        org.Id,
			Name:      org.Name,
		})

		return err
	})
}

func UpdateOrg(cmd *m.UpdateOrgCommand) error {
	return inTransaction(func(sess *DBSession) error {

		if isNameTaken, err := isOrgNameTaken(cmd.Name, cmd.OrgId, sess); err != nil {
			return err
		} else if isNameTaken {
			return m.ErrOrgNameTaken
		}

		org := m.Org{
			Name:    cmd.Name,
			Updated: time.Now(),
		}

		affectedRows, err := sess.ID(cmd.OrgId).Update(&org)

		if err != nil {
			return err
		}

		if affectedRows == 0 {
			return m.ErrOrgNotFound
		}

		sess.publishAfterCommit(&events.OrgUpdated{
			Timestamp: org.Updated,
			Id:        org.Id,
			Name:      org.Name,
		})

		return nil
	})
}

func UpdateOrgAddress(cmd *m.UpdateOrgAddressCommand) error {
	return inTransaction(func(sess *DBSession) error {
		org := m.Org{
			Address1: cmd.Address1,
			Address2: cmd.Address2,
			City:     cmd.City,
			ZipCode:  cmd.ZipCode,
			State:    cmd.State,
			Country:  cmd.Country,

			Updated: time.Now(),
		}

		if _, err := sess.ID(cmd.OrgId).Update(&org); err != nil {
			return err
		}

		sess.publishAfterCommit(&events.OrgUpdated{
			Timestamp: org.Updated,
			Id:        org.Id,
			Name:      org.Name,
		})

		return nil
	})
}

func DeleteOrg(cmd *m.DeleteOrgCommand) error {
	return inTransaction(func(sess *DBSession) error {
		if res, err := sess.Query("SELECT 1 from org WHERE id=?", cmd.Id); err != nil {
			return err
		} else if len(res) != 1 {
			return m.ErrOrgNotFound
		}

		deletes := []string{
			"DELETE FROM star WHERE EXISTS (SELECT 1 FROM dashboard WHERE org_id = ? AND star.dashboard_id = dashboard.id)",
			"DELETE FROM dashboard_tag WHERE EXISTS (SELECT 1 FROM dashboard WHERE org_id = ? AND dashboard_tag.dashboard_id = dashboard.id)",
			"DELETE FROM dashboard WHERE org_id = ?",
			"DELETE FROM api_key WHERE org_id = ?",
			"DELETE FROM data_source WHERE org_id = ?",
			"DELETE FROM org_user WHERE org_id = ?",
			"DELETE FROM org WHERE id = ?",
			"DELETE FROM temp_user WHERE org_id = ?",
		}

		for _, sql := range deletes {
			_, err := sess.Exec(sql, cmd.Id)
			if err != nil {
				return err
			}
		}

		return nil
	})
}
