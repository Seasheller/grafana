package datamigrations

import (
	"testing"
	"time"

	"github.com/Seasheller/grafana/pkg/cmd/grafana-cli/commands/commandstest"
	"github.com/Seasheller/grafana/pkg/components/securejsondata"
	"github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/services/sqlstore"
	"github.com/stretchr/testify/assert"
)

func TestPasswordMigrationCommand(t *testing.T) {
	//setup datasources with password, basic_auth and none
	sqlstore := sqlstore.InitTestDB(t)
	session := sqlstore.NewSession()
	defer session.Close()

	datasources := []*models.DataSource{
		{Type: "influxdb", Name: "influxdb", Password: "foobar"},
		{Type: "graphite", Name: "graphite", BasicAuthPassword: "foobar"},
		{Type: "prometheus", Name: "prometheus"},
		{Type: "elasticsearch", Name: "elasticsearch", Password: "pwd"},
	}

	// set required default values
	for _, ds := range datasources {
		ds.Created = time.Now()
		ds.Updated = time.Now()
		if ds.Name == "elasticsearch" {
			ds.SecureJsonData = securejsondata.GetEncryptedJsonData(map[string]string{
				"key": "value",
			})
		} else {
			ds.SecureJsonData = securejsondata.GetEncryptedJsonData(map[string]string{})
		}
	}

	_, err := session.Insert(&datasources)
	assert.Nil(t, err)

	// force secure_json_data to be null to verify that migration can handle that
	_, err = session.Exec("update data_source set secure_json_data = null where name = 'influxdb'")
	assert.Nil(t, err)

	//run migration
	err = EncryptDatasourcePaswords(&commandstest.FakeCommandLine{}, sqlstore)
	assert.Nil(t, err)

	//verify that no datasources still have password or basic_auth
	var dss []*models.DataSource
	err = session.SQL("select * from data_source").Find(&dss)
	assert.Nil(t, err)
	assert.Equal(t, len(dss), 4)

	for _, ds := range dss {
		sj := ds.SecureJsonData.Decrypt()

		if ds.Name == "influxdb" {
			assert.Equal(t, ds.Password, "")
			v, exist := sj["password"]
			assert.True(t, exist)
			assert.Equal(t, v, "foobar", "expected password to be moved to securejson")
		}

		if ds.Name == "graphite" {
			assert.Equal(t, ds.BasicAuthPassword, "")
			v, exist := sj["basicAuthPassword"]
			assert.True(t, exist)
			assert.Equal(t, v, "foobar", "expected basic_auth_password to be moved to securejson")
		}

		if ds.Name == "prometheus" {
			assert.Equal(t, len(sj), 0)
		}

		if ds.Name == "elasticsearch" {
			assert.Equal(t, ds.Password, "")
			key, exist := sj["key"]
			assert.True(t, exist)
			password, exist := sj["password"]
			assert.True(t, exist)
			assert.Equal(t, password, "pwd", "expected password to be moved to securejson")
			assert.Equal(t, key, "value", "expected existing key to be kept intact in securejson")
		}
	}
}
