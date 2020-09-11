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
	"github.com/google/ts-bridge/storage"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	log "github.com/sirupsen/logrus"
	ddapi "github.com/zorkian/go-datadog-api"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// Metric defines a Datadog-based metric. It implements the SourceMetric interface.
type Metric struct {
	Name                 string
	config               *MetricConfig
	client               *ddapi.Client
	minPointAge          time.Duration
	counterResetInterval time.Duration
}

// MetricConfig defines configuration file parameters for a specific metric imported from Datadog.
type MetricConfig struct {
	APIKey         string `yaml:"api_key" validate:"nonzero"`
	ApplicationKey string `yaml:"application_key" validate:"nonzero"`
	Query          string `validate:"nonzero"`
	Cumulative     bool
}

// NewSourceMetric creates a new SourceMetric from a metric name and configuration parameters.
func NewSourceMetric(name string, config *MetricConfig, minPointAge, counterResetInterval time.Duration) (*Metric, error) {
	if config.Cumulative && !strings.Contains(config.Query, "cumsum") {
		return nil, fmt.Errorf("Query for the cumulative metric %s does not contain the cumsum Datadog function", name)
	}

	client := ddapi.NewClient(config.APIKey, config.ApplicationKey)
	return &Metric{
		Name:                 name,
		config:               config,
		client:               client,
		minPointAge:          minPointAge,
		counterResetInterval: counterResetInterval,
	}, nil
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
// Time series data will include points after the given lastPoint timestamp.
func (m *Metric) StackdriverData(ctx context.Context, lastPoint time.Time, rec storage.MetricRecord) (*metricpb.MetricDescriptor, []*monitoringpb.TimeSeries, error) {
	// Datadog's `from` parameter is inclusive, so we set it to 1 second after the latest point we've got.
	from := lastPoint.Add(time.Second)
	if m.config.Cumulative {
		var err error
		from, err = m.counterStartTime(ctx, lastPoint, rec)
		if err != nil {
			return nil, nil, err
		}
	}

	series, err := m.client.QueryMetrics(from.Unix(), time.Now().Unix(), m.config.Query)
	if err != nil {
		return nil, nil, err
	}
	if len(series) == 0 {
		log.WithContext(ctx).Infof("Datadog query %q returned no time series", m.config.Query)
		return nil, nil, nil
	} else if len(series) > 1 {
		return nil, nil, fmt.Errorf("Datadog query %q returned %d time series", m.config.Query, len(series))
	}

	points, err := m.filterPoints(lastPoint, series[0].Points)
	log.WithContext(ctx).Debugf("Got %d points (%d after filtering) in response to the Datadog query %q", len(series[0].Points), len(points), m.config.Query)

	startTime, err := ptypes.TimestampProto(from)
	if err != nil {
		return nil, nil, fmt.Errorf("Count not convert timestamp %v to proto: %v", from, err)
	}
	return m.metricDescriptor(series[0]), m.convertTimeSeries(startTime, points), nil
}

// counterStartTime returns the start time for a cumulative metric. It's used as
// the `from` parameter while issuing Datadog queries, and also as the `start
// time` field in points reported for this cumulative metric to SD.
func (m *Metric) counterStartTime(ctx context.Context, lastPoint time.Time, rec storage.MetricRecord) (time.Time, error) {
	// Start time needs to be reset regularly, since otherwise we will be querying
	// Datadog for a time window large enough for aggregation to kick in.
	if time.Now().Sub(rec.GetCounterStartTime()) > m.counterResetInterval {
		var start time.Time
		if time.Now().Sub(lastPoint) <= m.counterResetInterval {
			// This is the common case: choose the new start time based on the last point
			// timestamp. This ensures continuity of data.
			// Datadog's timestamps have 1-second granularity, and timestamp X covers
			// data between X and X+1s, so we increment last point timestamp by 1 second
			// while choosing a new start time.
			start = lastPoint.Add(time.Second)
		} else {
			// This is the rare case: when last point is too old, we cannot use it as a
			// basis for new start time, since it will make new start time still older
			// than ResetInterval, and it will immediately need to be moved forward
			// again. This only happens when a new metric is added, or when writes to
			// Stackdriver have been failing for more than ResetInterval.
			// We need to choose an arbitrary point in the recent past as the new start
			// time, and we select half of the reset interval: this ensures that we
			// backfill some data, but won't need to reset the start time again for a
			// while.
			start = time.Now().Add(-m.counterResetInterval / 2)
		}
		if err := rec.SetCounterStartTime(ctx, start); err != nil {
			return time.Time{}, fmt.Errorf("Could not set counter start time: %v", err)
		}
		log.WithContext(ctx).Infof("Counter start time for %s has been reset to %v", m.Name, start)
	}
	return rec.GetCounterStartTime(), nil
}

// filterPoints gets a slice of Datadog points, and returns a similar slice, but without points that are too fresh or
// too old.
func (m *Metric) filterPoints(lastPoint time.Time, points []ddapi.DataPoint) ([]ddapi.DataPoint, error) {
	var output []ddapi.DataPoint
	for _, p := range points {
		ts, err := ptypes.Timestamp(pointTimestamp(p))
		if err != nil {
			return nil, fmt.Errorf("Could not parse point timestamp for %v: %v", p, err)
		}
		// Ignore points that are too fresh, since they might contain incomplete data.
		if time.Now().Sub(ts) < m.minPointAge {
			continue
		}
		// Ignore points that are equal to or older than the last written point. For gauge metrics this is a noop,
		// since we only query Datadog for new points, but for cumulative metrics this is where we discard already
		// written data (we still need to pull it from Datadog for it to return us a cumulative sum).
		if lastPoint.Sub(ts) >= 0 {
			continue
		}
		output = append(output, p)
	}
	return output, nil
}

// sdMetricKind returns Stackdriver metric kind for this metric.
func (m *Metric) metricKind() metricpb.MetricDescriptor_MetricKind {
	if m.config.Cumulative {
		return metricpb.MetricDescriptor_CUMULATIVE
	}
	return metricpb.MetricDescriptor_GAUGE
}

// metricDescriptor creates a Stackdriver MetricDescriptor based on a Datadog series.
func (m *Metric) metricDescriptor(series ddapi.Series) *metricpb.MetricDescriptor {
	d := &metricpb.MetricDescriptor{
		// Name does not need to be set here; it will be set by Stackdriver Adapter based on the Stackdriver
		// project that this metric is written to.
		Type:       m.StackdriverName(),
		MetricKind: m.metricKind(),
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
func (m *Metric) convertTimeSeries(start *timestamp.Timestamp, points []ddapi.DataPoint) []*monitoringpb.TimeSeries {
	ts := make([]*monitoringpb.TimeSeries, 0, len(points))
	for _, p := range points {
		ts = append(ts, &monitoringpb.TimeSeries{
			Metric:     &metricpb.Metric{Type: m.StackdriverName()},
			Resource:   &monitoredres.MonitoredResource{Type: "global"},
			MetricKind: m.metricKind(),
			ValueType:  metricpb.MetricDescriptor_DOUBLE,
			Points:     []*monitoringpb.Point{m.convertPoint(start, p)},
		})
	}
	return ts
}

// convertPoint converts a Datadog point into a Stackdriver point.
func (m *Metric) convertPoint(start *timestamp.Timestamp, p ddapi.DataPoint) *monitoringpb.Point {
	i := &monitoringpb.TimeInterval{EndTime: pointTimestamp(p)}
	if m.config.Cumulative {
		i.StartTime = start
	}
	return &monitoringpb.Point{
		Interval: i,
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
