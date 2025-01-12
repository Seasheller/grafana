package notifiers

import (
	"github.com/Seasheller/grafana/pkg/bus"
	"github.com/Seasheller/grafana/pkg/components/simplejson"
	"github.com/Seasheller/grafana/pkg/infra/log"
	"github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/services/alerting"
)

func init() {
	alerting.RegisterNotifier(&alerting.NotifierPlugin{
		Type:        "webhook",
		Name:        "webhook",
		Description: "Sends HTTP POST request to a URL",
		Factory:     NewWebHookNotifier,
		OptionsTemplate: `
      <h3 class="page-heading">Webhook settings</h3>
      <div class="gf-form">
        <span class="gf-form-label width-10">Url</span>
        <input type="text" required class="gf-form-input max-width-26" ng-model="ctrl.model.settings.url"></input>
      </div>
      <div class="gf-form">
        <span class="gf-form-label width-10">Http Method</span>
        <div class="gf-form-select-wrapper width-14">
          <select class="gf-form-input" ng-model="ctrl.model.settings.httpMethod" ng-options="t for t in ['POST', 'PUT']">
          </select>
        </div>
      </div>
      <div class="gf-form">
        <span class="gf-form-label width-10">Username</span>
        <input type="text" class="gf-form-input max-width-14" ng-model="ctrl.model.settings.username"></input>
      </div>
      <div class="gf-form">
        <span class="gf-form-label width-10">Password</span>
        <input type="text" class="gf-form-input max-width-14" ng-model="ctrl.model.settings.password"></input>
      </div>
    `,
	})

}

// NewWebHookNotifier is the constructor for
// the WebHook notifier.
func NewWebHookNotifier(model *models.AlertNotification) (alerting.Notifier, error) {
	url := model.Settings.Get("url").MustString()
	if url == "" {
		return nil, alerting.ValidationError{Reason: "Could not find url property in settings"}
	}

	return &WebhookNotifier{
		NotifierBase: NewNotifierBase(model),
		URL:          url,
		User:         model.Settings.Get("username").MustString(),
		Password:     model.Settings.Get("password").MustString(),
		HTTPMethod:   model.Settings.Get("httpMethod").MustString("POST"),
		log:          log.New("alerting.notifier.webhook"),
	}, nil
}

// WebhookNotifier is responsible for sending
// alert notifications as webhooks.
type WebhookNotifier struct {
	NotifierBase
	URL        string
	User       string
	Password   string
	HTTPMethod string
	log        log.Logger
}

// Notify send alert notifications as
// webhook as http requests.
func (wn *WebhookNotifier) Notify(evalContext *alerting.EvalContext) error {
	wn.log.Info("Sending webhook")

	bodyJSON := simplejson.New()
	bodyJSON.Set("title", evalContext.GetNotificationTitle())
	bodyJSON.Set("ruleId", evalContext.Rule.ID)
	bodyJSON.Set("ruleName", evalContext.Rule.Name)
	bodyJSON.Set("state", evalContext.Rule.State)
	bodyJSON.Set("evalMatches", evalContext.EvalMatches)

	ruleURL, err := evalContext.GetRuleURL()
	if err == nil {
		bodyJSON.Set("ruleUrl", ruleURL)
	}

	if evalContext.ImagePublicURL != "" {
		bodyJSON.Set("imageUrl", evalContext.ImagePublicURL)
	}

	if evalContext.Rule.Message != "" {
		bodyJSON.Set("message", evalContext.Rule.Message)
	}

	body, _ := bodyJSON.MarshalJSON()

	cmd := &models.SendWebhookSync{
		Url:        wn.URL,
		User:       wn.User,
		Password:   wn.Password,
		Body:       string(body),
		HttpMethod: wn.HTTPMethod,
	}

	if err := bus.DispatchCtx(evalContext.Ctx, cmd); err != nil {
		wn.log.Error("Failed to send webhook", "error", err, "webhook", wn.Name)
		return err
	}

	return nil
}
