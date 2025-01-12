package api

import (
	"regexp"
	"strings"

	"github.com/Seasheller/grafana/pkg/bus"
	m "github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/setting"
)

func AdminGetSettings(c *m.ReqContext) {
	settings := make(map[string]interface{})

	for _, section := range setting.Raw.Sections() {
		jsonSec := make(map[string]interface{})
		settings[section.Name()] = jsonSec

		for _, key := range section.Keys() {
			keyName := key.Name()
			value := key.Value()
			if strings.Contains(keyName, "secret") || strings.Contains(keyName, "password") || (strings.Contains(keyName, "provider_config")) {
				value = "************"
			}
			if strings.Contains(keyName, "url") {
				var rgx = regexp.MustCompile(`.*:\/\/([^:]*):([^@]*)@.*?$`)
				var subs = rgx.FindAllSubmatch([]byte(value), -1)
				if subs != nil && len(subs[0]) == 3 {
					value = strings.Replace(value, string(subs[0][1]), "******", 1)
					value = strings.Replace(value, string(subs[0][2]), "******", 1)
				}
			}

			jsonSec[keyName] = value
		}
	}

	c.JSON(200, settings)
}

func AdminGetStats(c *m.ReqContext) {

	statsQuery := m.GetAdminStatsQuery{}

	if err := bus.Dispatch(&statsQuery); err != nil {
		c.JsonApiErr(500, "Failed to get admin stats from database", err)
		return
	}

	c.JSON(200, statsQuery.Result)
}
