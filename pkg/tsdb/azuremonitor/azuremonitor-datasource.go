package azuremonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Seasheller/grafana/pkg/api/pluginproxy"
	"github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/plugins"
	"github.com/Seasheller/grafana/pkg/setting"
	opentracing "github.com/opentracing/opentracing-go"
	"golang.org/x/net/context/ctxhttp"

	"github.com/Seasheller/grafana/pkg/components/null"
	"github.com/Seasheller/grafana/pkg/components/simplejson"
	"github.com/Seasheller/grafana/pkg/tsdb"
)

// AzureMonitorDatasource calls the Azure Monitor API - one of the four API's supported
type AzureMonitorDatasource struct {
	httpClient *http.Client
	dsInfo     *models.DataSource
}

var (
	// 1m, 5m, 15m, 30m, 1h, 6h, 12h, 1d in milliseconds
	defaultAllowedIntervalsMS = []int64{60000, 300000, 900000, 1800000, 3600000, 21600000, 43200000, 86400000}
)

// executeTimeSeriesQuery does the following:
// 1. build the AzureMonitor url and querystring for each query
// 2. executes each query by calling the Azure Monitor API
// 3. parses the responses for each query into the timeseries format
func (e *AzureMonitorDatasource) executeTimeSeriesQuery(ctx context.Context, originalQueries []*tsdb.Query, timeRange *tsdb.TimeRange) (*tsdb.Response, error) {
	result := &tsdb.Response{
		Results: map[string]*tsdb.QueryResult{},
	}

	queries, err := e.buildQueries(originalQueries, timeRange)
	if err != nil {
		return nil, err
	}

	for _, query := range queries {
		queryRes, resp, err := e.executeQuery(ctx, query, originalQueries, timeRange)
		if err != nil {
			return nil, err
		}
		// azlog.Debug("AzureMonitor", "Response", resp)

		err = e.parseResponse(queryRes, resp, query)
		if err != nil {
			queryRes.Error = err
		}
		result.Results[query.RefID] = queryRes
	}

	return result, nil
}

func (e *AzureMonitorDatasource) buildQueries(queries []*tsdb.Query, timeRange *tsdb.TimeRange) ([]*AzureMonitorQuery, error) {
	azureMonitorQueries := []*AzureMonitorQuery{}
	startTime, err := timeRange.ParseFrom()
	if err != nil {
		return nil, err
	}

	endTime, err := timeRange.ParseTo()
	if err != nil {
		return nil, err
	}

	for _, query := range queries {
		var target string

		azureMonitorTarget := query.Model.Get("azureMonitor").MustMap()
		azlog.Debug("AzureMonitor", "target", azureMonitorTarget)

		urlComponents := map[string]string{}
		urlComponents["subscription"] = fmt.Sprintf("%v", query.Model.Get("subscription").MustString())
		urlComponents["resourceGroup"] = fmt.Sprintf("%v", azureMonitorTarget["resourceGroup"])
		urlComponents["metricDefinition"] = fmt.Sprintf("%v", azureMonitorTarget["metricDefinition"])
		urlComponents["resourceName"] = fmt.Sprintf("%v", azureMonitorTarget["resourceName"])

		ub := urlBuilder{
			DefaultSubscription: query.DataSource.JsonData.Get("subscriptionId").MustString(),
			Subscription:        urlComponents["subscription"],
			ResourceGroup:       urlComponents["resourceGroup"],
			MetricDefinition:    urlComponents["metricDefinition"],
			ResourceName:        urlComponents["resourceName"],
		}
		azureURL := ub.Build()

		alias := ""
		if val, ok := azureMonitorTarget["alias"]; ok {
			alias = fmt.Sprintf("%v", val)
		}

		timeGrain := fmt.Sprintf("%v", azureMonitorTarget["timeGrain"])
		timeGrains := azureMonitorTarget["allowedTimeGrainsMs"]
		if timeGrain == "auto" {
			timeGrain, err = e.setAutoTimeGrain(query.IntervalMs, timeGrains)
			if err != nil {
				return nil, err
			}
		}

		params := url.Values{}
		params.Add("api-version", "2018-01-01")
		params.Add("timespan", fmt.Sprintf("%v/%v", startTime.UTC().Format(time.RFC3339), endTime.UTC().Format(time.RFC3339)))
		params.Add("interval", timeGrain)
		params.Add("aggregation", fmt.Sprintf("%v", azureMonitorTarget["aggregation"]))
		params.Add("metricnames", fmt.Sprintf("%v", azureMonitorTarget["metricName"]))
		params.Add("metricnamespace", fmt.Sprintf("%v", azureMonitorTarget["metricNamespace"]))

		dimension := strings.TrimSpace(fmt.Sprintf("%v", azureMonitorTarget["dimension"]))
		dimensionFilter := strings.TrimSpace(fmt.Sprintf("%v", azureMonitorTarget["dimensionFilter"]))
		if azureMonitorTarget["dimension"] != nil && azureMonitorTarget["dimensionFilter"] != nil && len(dimension) > 0 && len(dimensionFilter) > 0 && dimension != "None" {
			params.Add("$filter", fmt.Sprintf("%s eq '%s'", dimension, dimensionFilter))
		}

		target = params.Encode()

		if setting.Env == setting.DEV {
			azlog.Debug("Azuremonitor request", "params", params)
		}

		azureMonitorQueries = append(azureMonitorQueries, &AzureMonitorQuery{
			URL:           azureURL,
			UrlComponents: urlComponents,
			Target:        target,
			Params:        params,
			RefID:         query.RefId,
			Alias:         alias,
		})
	}

	return azureMonitorQueries, nil
}

// setAutoTimeGrain tries to find the closest interval to the query's intervalMs value
// if the metric has a limited set of possible intervals/time grains then use those
// instead of the default list of intervals
func (e *AzureMonitorDatasource) setAutoTimeGrain(intervalMs int64, timeGrains interface{}) (string, error) {
	// parses array of numbers from the timeGrains json field
	allowedTimeGrains := []int64{}
	tgs, ok := timeGrains.([]interface{})
	if ok {
		for _, v := range tgs {
			jsonNumber, ok := v.(json.Number)
			if ok {
				tg, err := jsonNumber.Int64()
				if err == nil {
					allowedTimeGrains = append(allowedTimeGrains, tg)
				}
			}
		}
	}

	autoInterval := e.findClosestAllowedIntervalMS(intervalMs, allowedTimeGrains)
	tg := &TimeGrain{}
	autoTimeGrain, err := tg.createISO8601DurationFromIntervalMS(autoInterval)
	if err != nil {
		return "", err
	}

	return autoTimeGrain, nil
}

func (e *AzureMonitorDatasource) executeQuery(ctx context.Context, query *AzureMonitorQuery, queries []*tsdb.Query, timeRange *tsdb.TimeRange) (*tsdb.QueryResult, AzureMonitorResponse, error) {
	queryResult := &tsdb.QueryResult{Meta: simplejson.New(), RefId: query.RefID}

	req, err := e.createRequest(ctx, e.dsInfo)
	if err != nil {
		queryResult.Error = err
		return queryResult, AzureMonitorResponse{}, nil
	}

	req.URL.Path = path.Join(req.URL.Path, query.URL)
	req.URL.RawQuery = query.Params.Encode()
	queryResult.Meta.Set("rawQuery", req.URL.RawQuery)

	span, ctx := opentracing.StartSpanFromContext(ctx, "azuremonitor query")
	span.SetTag("target", query.Target)
	span.SetTag("from", timeRange.From)
	span.SetTag("until", timeRange.To)
	span.SetTag("datasource_id", e.dsInfo.Id)
	span.SetTag("org_id", e.dsInfo.OrgId)

	defer span.Finish()

	opentracing.GlobalTracer().Inject(
		span.Context(),
		opentracing.HTTPHeaders,
		opentracing.HTTPHeadersCarrier(req.Header))

	azlog.Debug("AzureMonitor", "Request URL", req.URL.String())
	res, err := ctxhttp.Do(ctx, e.httpClient, req)
	if err != nil {
		queryResult.Error = err
		return queryResult, AzureMonitorResponse{}, nil
	}

	data, err := e.unmarshalResponse(res)
	if err != nil {
		queryResult.Error = err
		return queryResult, AzureMonitorResponse{}, nil
	}

	return queryResult, data, nil
}

func (e *AzureMonitorDatasource) createRequest(ctx context.Context, dsInfo *models.DataSource) (*http.Request, error) {
	// find plugin
	plugin, ok := plugins.DataSources[dsInfo.Type]
	if !ok {
		return nil, errors.New("Unable to find datasource plugin Azure Monitor")
	}

	var azureMonitorRoute *plugins.AppPluginRoute
	for _, route := range plugin.Routes {
		if route.Path == "azuremonitor" {
			azureMonitorRoute = route
			break
		}
	}

	cloudName := dsInfo.JsonData.Get("cloudName").MustString("azuremonitor")
	proxyPass := fmt.Sprintf("%s/subscriptions", cloudName)

	u, _ := url.Parse(dsInfo.Url)
	u.Path = path.Join(u.Path, "render")

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		azlog.Error("Failed to create request", "error", err)
		return nil, fmt.Errorf("Failed to create request. error: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("Grafana/%s", setting.BuildVersion))

	pluginproxy.ApplyRoute(ctx, req, proxyPass, azureMonitorRoute, dsInfo)

	return req, nil
}

func (e *AzureMonitorDatasource) unmarshalResponse(res *http.Response) (AzureMonitorResponse, error) {
	body, err := ioutil.ReadAll(res.Body)
	defer res.Body.Close()
	if err != nil {
		return AzureMonitorResponse{}, err
	}

	if res.StatusCode/100 != 2 {
		azlog.Error("Request failed", "status", res.Status, "body", string(body))
		return AzureMonitorResponse{}, fmt.Errorf(string(body))
	}

	var data AzureMonitorResponse
	err = json.Unmarshal(body, &data)
	if err != nil {
		azlog.Error("Failed to unmarshal AzureMonitor response", "error", err, "status", res.Status, "body", string(body))
		return AzureMonitorResponse{}, err
	}

	return data, nil
}

func (e *AzureMonitorDatasource) parseResponse(queryRes *tsdb.QueryResult, data AzureMonitorResponse, query *AzureMonitorQuery) error {
	if len(data.Value) == 0 {
		return nil
	}

	for _, series := range data.Value[0].Timeseries {
		points := []tsdb.TimePoint{}

		metadataName := ""
		metadataValue := ""
		if len(series.Metadatavalues) > 0 {
			metadataName = series.Metadatavalues[0].Name.LocalizedValue
			metadataValue = series.Metadatavalues[0].Value
		}
		metricName := formatLegendKey(query.Alias, query.UrlComponents["resourceName"], data.Value[0].Name.LocalizedValue, metadataName, metadataValue, data.Namespace, data.Value[0].ID)

		for _, point := range series.Data {
			var value float64
			switch query.Params.Get("aggregation") {
			case "Average":
				value = point.Average
			case "Total":
				value = point.Total
			case "Maximum":
				value = point.Maximum
			case "Minimum":
				value = point.Minimum
			case "Count":
				value = point.Count
			default:
				value = point.Count
			}
			points = append(points, tsdb.NewTimePoint(null.FloatFrom(value), float64((point.TimeStamp).Unix())*1000))
		}

		queryRes.Series = append(queryRes.Series, &tsdb.TimeSeries{
			Name:   metricName,
			Points: points,
		})
	}
	queryRes.Meta.Set("unit", data.Value[0].Unit)

	return nil
}

// findClosestAllowedIntervalMs is used for the auto time grain setting.
// It finds the closest time grain from the list of allowed time grains for Azure Monitor
// using the Grafana interval in milliseconds
// Some metrics only allow a limited list of time grains. The allowedTimeGrains parameter
// allows overriding the default list of allowed time grains.
func (e *AzureMonitorDatasource) findClosestAllowedIntervalMS(intervalMs int64, allowedTimeGrains []int64) int64 {
	allowedIntervals := defaultAllowedIntervalsMS

	if len(allowedTimeGrains) > 0 {
		allowedIntervals = allowedTimeGrains
	}

	closest := allowedIntervals[0]

	for i, allowed := range allowedIntervals {
		if intervalMs > allowed {
			if i+1 < len(allowedIntervals) {
				closest = allowedIntervals[i+1]
			} else {
				closest = allowed
			}
		}
	}
	return closest
}

// formatLegendKey builds the legend key or timeseries name
// Alias patterns like {{resourcename}} are replaced with the appropriate data values.
func formatLegendKey(alias string, resourceName string, metricName string, metadataName string, metadataValue string, namespace string, seriesID string) string {
	if alias == "" {
		if len(metadataName) > 0 {
			return fmt.Sprintf("%s{%s=%s}.%s", resourceName, metadataName, metadataValue, metricName)
		}
		return fmt.Sprintf("%s.%s", resourceName, metricName)
	}

	startIndex := strings.Index(seriesID, "/resourceGroups/") + 16
	endIndex := strings.Index(seriesID, "/providers")
	resourceGroup := seriesID[startIndex:endIndex]

	result := legendKeyFormat.ReplaceAllFunc([]byte(alias), func(in []byte) []byte {
		metaPartName := strings.Replace(string(in), "{{", "", 1)
		metaPartName = strings.Replace(metaPartName, "}}", "", 1)
		metaPartName = strings.ToLower(strings.TrimSpace(metaPartName))

		if metaPartName == "resourcegroup" {
			return []byte(resourceGroup)
		}

		if metaPartName == "namespace" {
			return []byte(namespace)
		}

		if metaPartName == "resourcename" {
			return []byte(resourceName)
		}

		if metaPartName == "metric" {
			return []byte(metricName)
		}

		if metaPartName == "dimensionname" {
			return []byte(metadataName)
		}

		if metaPartName == "dimensionvalue" {
			return []byte(metadataValue)
		}

		return in
	})

	return string(result)
}
