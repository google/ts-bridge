// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package newrelic

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/google/ts-bridge/record"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/appengine/log"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// MetricConfig defines configuration file parameters for a specific metric imported from New Relic.
type MetricConfig struct {
	APIKey        string `yaml:"api_key" validate:"nonzero"`
	ApplicationId string `yaml:"application_id" validate:"nonzero"`
	MetricName    string `validate:"nonzero" yaml:"metric_name"`
	MetricValue   string `validate:"nonzero" yaml:"metric_value"`
	Cumulative    bool
}

// Metric defines a New Relic-based metric. It implements the SourceMetric interface.
type Metric struct {
	Name   string
	config *MetricConfig
	client *Client
}

type timeslice struct {
	From   string             `json:"from"`
	To     string             `json:"to"`
	Values map[string]float64 `json:"values"`
}
type data struct {
	Name       string      `json:"name"`
	Timeslices []timeslice `json:"timeslices"`
}
type metricData struct {
	From            string   `json:"from"`
	To              string   `json:"to"`
	MetricsNotFound []string `json:"metrics_not_found,omitempty"`
	MetricsFound    []string `json:"metrics_found,omitempty"`
	Metrics         []data   `json:"metrics`
}
type responseData struct {
	MetricData metricData `json:"metric_data"`
}

// Client defines the information necessary to query New Relic
type Client struct {
	APIKey        string
	ApplicationId string
	BaseUrl       string
}

// Query returns a unique name for the query
func (m *Metric) Query() string {
	return fmt.Sprintf("%v:%v", m.config.MetricName, m.config.MetricValue)
}

// QueryMetrics returns a metric descriptor and a slice of TimeSeries for the metric.
// all elements of the TimeSeries must be the same metric descriptor and contain a single point
func (m *Metric) QueryMetrics(ctx context.Context, start time.Time, end time.Time, name string, value string) (*metricpb.MetricDescriptor, []*monitoringpb.TimeSeries, error) {
	c := m.client

	var ret []*monitoringpb.TimeSeries
	tmpStart := start

	for continueLoop := true; continueLoop; {
		// Always use 3 hour windows, anything more and points will get aggregated to greater than 1 minute granularity
		tmpEnd := tmpStart.Add(3 * time.Hour)
		if tmpEnd.After(end) {
			tmpEnd = end
			continueLoop = false
		}

		// TODO: add multi-page support
		url := fmt.Sprintf("%s/v2/applications/%v/metrics/data.json",
			c.BaseUrl, c.ApplicationId)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, nil, err
		}
		q := req.URL.Query()
		q.Add("names[]", name)
		q.Add("values[]", value)
		q.Add("from", tmpStart.Format(time.RFC3339))
		q.Add("to", tmpEnd.Format(time.RFC3339))

		req.URL.RawQuery = q.Encode()
		req.Header.Set("X-Api-Key", c.APIKey)
		httpClient := &http.Client{}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, err
		}

		var bodydata responseData
		err = json.Unmarshal(body, &bodydata)
		if err != nil {
			return nil, nil, err
		}
		ts := m.convertTimeSeries(ctx, bodydata, value)

		if ret == nil || len(ret) == 0 {
			ret = ts
		} else {
			if len(ts) > 0 {
				ret[0].Points = append(ret[0].Points, ts[0].Points...)
			}
		}

		tmpStart = tmpEnd
	}
	descriptor := &metricpb.MetricDescriptor{
		Name:        m.StackdriverName(),
		MetricKind:  metricpb.MetricDescriptor_GAUGE,
		ValueType:   metricpb.MetricDescriptor_DOUBLE,
		Description: fmt.Sprintf("New Relic Query for name: %v, value %v\n", name, value),
		DisplayName: fmt.Sprintf("%v:%v", name, value),
		Type:        m.StackdriverName(),
	}
	return descriptor, ret, nil
}

func (m *Metric) convertTimeSeries(ctx context.Context, data responseData, value string) []*monitoringpb.TimeSeries {
	var ts []*monitoringpb.TimeSeries

	timeslices := data.MetricData.Metrics[0].Timeslices
	if len(timeslices) == 0 {
		return nil
	}

	for _, slice := range timeslices {
		val, ok := slice.Values[value]
		if !ok {
			continue
		}

		toTime, err := time.Parse(time.RFC3339, slice.To)
		if err != nil {
			log.Errorf(ctx, "unable to parse from time %v\n", slice.To)
			continue
		}
		toTimestamp, err := ptypes.TimestampProto(toTime)
		if err != nil {
			fmt.Printf("unable to parse time to timestamp: %v", err)
			continue
		}
		ts = append(ts, &monitoringpb.TimeSeries{
			Metric:     &metricpb.Metric{Type: m.StackdriverName()},
			Resource:   &monitoredres.MonitoredResource{Type: "global"},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			ValueType:  metricpb.MetricDescriptor_DOUBLE,
			Points: []*monitoringpb.Point{&monitoringpb.Point{
				Interval: &monitoringpb.TimeInterval{
					//StartTime: fromTimestamp,
					EndTime: toTimestamp,
				},
				Value: &monitoringpb.TypedValue{
					Value: &monitoringpb.TypedValue_DoubleValue{DoubleValue: val},
				},
			}}})
	}
	return ts
}

// NewSourceMetric creates a new SourceMetric from a metric name and configuration parameters.
func NewSourceMetric(name string, config *MetricConfig) (*Metric, error) {
	client := Client{
		APIKey:        config.APIKey,
		ApplicationId: config.ApplicationId,
		BaseUrl:       "https://api.newrelic.com",
	}
	return &Metric{
		Name:   name,
		config: config,
		client: &client,
	}, nil
}

// StackdriverName returns the full Stackdriver metric name (also called "metric type") for this metric.
func (m *Metric) StackdriverName() string {
	return fmt.Sprintf("custom.googleapis.com/newrelic/%s", m.Name)
}

// StackdriverData issues a NewRelic query, returning metric descriptor and time series data.
// Time series data will include points after the given lastPoint timestamp.
func (m *Metric) StackdriverData(ctx context.Context, lastPoint time.Time, rec record.MetricRecord) (*metricpb.MetricDescriptor, []*monitoringpb.TimeSeries, error) {
	log.Errorf(ctx, "lastPoint: %v\n", lastPoint)
	desc, series, err := m.QueryMetrics(ctx, lastPoint, time.Now(), m.config.MetricName, m.config.MetricValue)
	if err != nil {
		return nil, nil, err
	}
	if len(series) == 0 {
		log.Infof(ctx, "Query '%s' returned no time series", m.Query())
	}

	return desc, series, nil
}
