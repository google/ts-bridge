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

package datadog

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	ddapi "github.com/zorkian/go-datadog-api"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// Metric defines a Datadog-based metric. It implements the SourceMetric interface.
type Metric struct {
	Name        string
	config      *MetricConfig
	client      *ddapi.Client
	minPointAge time.Duration
}

// MetricConfig defines configuration file parameters for a specific metric imported from Datadog.
type MetricConfig struct {
	APIKey         string `yaml:"api_key" validate:"nonzero"`
	ApplicationKey string `yaml:"application_key" validate:"nonzero"`
	Query          string `validate:"nonzero"`
}

// NewSourceMetric creates a new SourceMetric from a metric name and configuration parameters.
func NewSourceMetric(name string, config *MetricConfig, minPointAge time.Duration) *Metric {
	client := ddapi.NewClient(config.APIKey, config.ApplicationKey)
	return &Metric{
		Name:        name,
		config:      config,
		client:      client,
		minPointAge: minPointAge,
	}
}

// StackdriverName returns the full Stackdriver metric name (also called "metric type") for this metric.
func (m *Metric) StackdriverName() string {
	return fmt.Sprintf("custom.googleapis.com/datadog/%s", m.Name)
}

// Query returns the query being imported from Datadog.
func (m *Metric) Query() string {
	return m.config.Query
}

// StackdriverData issues a Datadog query, returning metric descriptor and time series data.
// Time series data will include points since the given timestamp.
func (m *Metric) StackdriverData(ctx context.Context, since time.Time) (*metricpb.MetricDescriptor, []*monitoringpb.TimeSeries, error) {
	m.client.HttpClient = urlfetch.Client(ctx)
	// Datadog's `from` parameter is inclusive, so we set it to 1 second after the latest point we've got.
	series, err := m.client.QueryMetrics(since.Unix()+1, time.Now().Unix(), m.config.Query)
	if err != nil {
		return nil, nil, err
	}
	if len(series) == 0 {
		log.Infof(ctx, "Query '%s' returned no time series", m.config.Query)
		return nil, nil, nil
	} else if len(series) > 1 {
		return nil, nil, fmt.Errorf("Query '%s' returned %d time series", m.config.Query, len(series))
	}
	points, err := m.filterPoints(series[0].Points)
	log.Debugf(ctx, "Got %d points (%d after filtering) in response to the Datadog query '%s'", len(series[0].Points), len(points), m.config.Query)
	return m.metricDescriptor(series[0]), m.convertTimeSeries(points), nil
}

// filterPoints gets a slice of Datadog points, and returns a similar slice, but without points that are too fresh.
func (m *Metric) filterPoints(points []ddapi.DataPoint) ([]ddapi.DataPoint, error) {
	var output []ddapi.DataPoint
	for _, p := range points {
		ts, err := ptypes.Timestamp(pointTimestamp(p))
		if err != nil {
			return nil, fmt.Errorf("Could not parse point timestamp for %v: %v", p, err)
		}
		if time.Now().Sub(ts) >= m.minPointAge {
			output = append(output, p)
		}
	}
	return output, nil
}

// metricDescriptor creates a Stackdriver MetricDescriptor based on a Datadog series.
func (m *Metric) metricDescriptor(series ddapi.Series) *metricpb.MetricDescriptor {
	d := &metricpb.MetricDescriptor{
		// Name does not need to be set here; it will be set by Stackdriver Adapter based on the Stackdriver
		// project that this metric is written to.
		Type: m.StackdriverName(),
		// Query results are gauges; there does not seem to be a way to get cumulative metrics from Datadog.
		MetricKind: metricpb.MetricDescriptor_GAUGE,
		// Datadog API does not declare value type, and the client library exposes all points as float64.
		ValueType:   metricpb.MetricDescriptor_DOUBLE,
		Description: fmt.Sprintf("Datadog query: %s", m.config.Query),
		DisplayName: *series.DisplayName,
	}
	if u, ok := series.GetUnitsOk(); ok {
		// Sometimes we get a slice of two pointers, but the second is nil.
		if len(u) == 2 && u[0] != nil && u[1] != nil {
			// Numerator and denominator.
			d.Unit = fmt.Sprintf("%s/%s", u[0].ShortName, u[1].ShortName)
		} else if len(u) > 0 && u[0] != nil {
			d.Unit = u[0].ShortName
		}
	}
	return d
}

// convertTimeSeries generates a slice of Stackdriver TimeSeries protos based on a Datadog Series.
// A separate TimeSeries message is created for each point because Stackdriver only allows sending a single
// point in a given request for each time series, so multiple points will need to be sent as separate requests.
// See https://cloud.google.com/monitoring/custom-metrics/creating-metrics#writing-ts
func (m *Metric) convertTimeSeries(points []ddapi.DataPoint) []*monitoringpb.TimeSeries {
	ts := make([]*monitoringpb.TimeSeries, 0, len(points))
	for _, p := range points {
		ts = append(ts, &monitoringpb.TimeSeries{
			Metric:     &metricpb.Metric{Type: m.StackdriverName()},
			Resource:   &monitoredres.MonitoredResource{Type: "global"},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			ValueType:  metricpb.MetricDescriptor_DOUBLE,
			Points:     []*monitoringpb.Point{convertPoint(p)},
		})
	}
	return ts
}

// convertPoint converts a Datadog point into a Stackdriver point.
func convertPoint(p ddapi.DataPoint) *monitoringpb.Point {
	return &monitoringpb.Point{
		Interval: &monitoringpb.TimeInterval{
			EndTime: pointTimestamp(p),
		},
		Value: &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_DoubleValue{
				DoubleValue: *p[1],
			},
		},
	}
}

func pointTimestamp(p ddapi.DataPoint) *timestamp.Timestamp {
	return &timestamp.Timestamp{
		// Datadog timestamps are in milliseconds.
		Seconds: int64(*p[0] / 1e3),
		Nanos:   int32(int64(*p[0]*1e6) % 1e9),
	}
}
