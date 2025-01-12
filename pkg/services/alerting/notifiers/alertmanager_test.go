package notifiers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Seasheller/grafana/pkg/components/simplejson"
	"github.com/Seasheller/grafana/pkg/infra/log"
	"github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/services/alerting"
	. "github.com/smartystreets/goconvey/convey"
)

func TestReplaceIllegalCharswithUnderscore(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{
			input:    "foobar",
			expected: "foobar",
		},
		{
			input:    `foo.,\][!?#="~*^&+|<>\'bar09_09`,
			expected: "foo____________________bar09_09",
		},
	}

	for _, c := range cases {
		assert.Equal(t, replaceIllegalCharsInLabelname(c.input), c.expected)
	}
}

func TestWhenAlertManagerShouldNotify(t *testing.T) {
	tcs := []struct {
		prevState models.AlertStateType
		newState  models.AlertStateType

		expect bool
	}{
		{
			prevState: models.AlertStatePending,
			newState:  models.AlertStateOK,
			expect:    false,
		},
		{
			prevState: models.AlertStateAlerting,
			newState:  models.AlertStateOK,
			expect:    true,
		},
		{
			prevState: models.AlertStateOK,
			newState:  models.AlertStatePending,
			expect:    false,
		},
		{
			prevState: models.AlertStateUnknown,
			newState:  models.AlertStatePending,
			expect:    false,
		},
	}

	for _, tc := range tcs {
		am := &AlertmanagerNotifier{log: log.New("test.logger")}
		evalContext := alerting.NewEvalContext(context.Background(), &alerting.Rule{
			State: tc.prevState,
		})

		evalContext.Rule.State = tc.newState

		res := am.ShouldNotify(context.TODO(), evalContext, &models.AlertNotificationState{})
		if res != tc.expect {
			t.Errorf("got %v expected %v", res, tc.expect)
		}
	}
}

//nolint:goconst
func TestAlertmanagerNotifier(t *testing.T) {
	Convey("Alertmanager notifier tests", t, func() {

		Convey("Parsing alert notification from settings", func() {
			Convey("empty settings should return error", func() {
				json := `{ }`

				settingsJSON, _ := simplejson.NewJson([]byte(json))
				model := &models.AlertNotification{
					Name:     "alertmanager",
					Type:     "alertmanager",
					Settings: settingsJSON,
				}

				_, err := NewAlertmanagerNotifier(model)
				So(err, ShouldNotBeNil)
			})

			Convey("from settings", func() {
				json := `{ "url": "http://127.0.0.1:9093/" }`

				settingsJSON, _ := simplejson.NewJson([]byte(json))
				model := &models.AlertNotification{
					Name:     "alertmanager",
					Type:     "alertmanager",
					Settings: settingsJSON,
				}

				not, err := NewAlertmanagerNotifier(model)
				alertmanagerNotifier := not.(*AlertmanagerNotifier)

				So(err, ShouldBeNil)
				So(alertmanagerNotifier.URL, ShouldEqual, "http://127.0.0.1:9093/")
			})
		})
	})
}
