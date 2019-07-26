package sqlstore

import (
	"github.com/Seasheller/grafana/pkg/bus"
	m "github.com/Seasheller/grafana/pkg/models"
)

func init() {
	bus.AddHandler("sql", GetDBHealthQuery)
}

func GetDBHealthQuery(query *m.GetDBHealthQuery) error {
	return x.Ping()
}
