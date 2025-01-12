package notifiers

import (
	"testing"

	"github.com/Seasheller/grafana/pkg/components/simplejson"
	"github.com/Seasheller/grafana/pkg/models"
	. "github.com/smartystreets/goconvey/convey"
)

func TestOpsGenieNotifier(t *testing.T) {
	Convey("OpsGenie notifier tests", t, func() {

		Convey("Parsing alert notification from settings", func() {
			Convey("empty settings should return error", func() {
				json := `{ }`

				settingsJSON, _ := simplejson.NewJson([]byte(json))
				model := &models.AlertNotification{
					Name:     "opsgenie_testing",
					Type:     "opsgenie",
					Settings: settingsJSON,
				}

				_, err := NewOpsGenieNotifier(model)
				So(err, ShouldNotBeNil)
			})

			Convey("settings should trigger incident", func() {
				json := `
				{
          "apiKey": "abcdefgh0123456789"
				}`

				settingsJSON, _ := simplejson.NewJson([]byte(json))
				model := &models.AlertNotification{
					Name:     "opsgenie_testing",
					Type:     "opsgenie",
					Settings: settingsJSON,
				}

				not, err := NewOpsGenieNotifier(model)
				opsgenieNotifier := not.(*OpsGenieNotifier)

				So(err, ShouldBeNil)
				So(opsgenieNotifier.Name, ShouldEqual, "opsgenie_testing")
				So(opsgenieNotifier.Type, ShouldEqual, "opsgenie")
				So(opsgenieNotifier.APIKey, ShouldEqual, "abcdefgh0123456789")
			})
		})
	})
}
