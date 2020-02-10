// Copyright 2020 Google LLC
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

package influxdb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/ts-bridge/record"

	"github.com/golang/protobuf/ptypes"
	"github.com/influxdata/influxdb1-client/models"
	client "github.com/influxdata/influxdb1-client/v2"
	"google.golang.org/appengine/log"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// By passing around a time function, we can easily stub time in tests.
var timeNow = time.Now

// Metric defines a InfluxDB-based metric. It implements the SourceMetric
// interface.
type Metric struct {
	Name                 string
	config               *MetricConfig
	offsetDuration       time.Duration
	counterResetInterval time.Duration
}

func NewSourceMetric(name string, config *MetricConfig, offsetDuration, counterResetInterval time.Duration) (*Metric, error) {
	if err := config.validateQuery(); err != nil {
		return nil, err
	}

	return &Metric{
		Name:                 name,
		config:               config,
		offsetDuration:       offsetDuration,
		counterResetInterval: counterResetInterval,
	}, nil
}

func (m *Metric) StackdriverName() string {
	return fmt.Sprintf("custom.googleapis.com/influxdb/%s", m.Name)
}

func (m *Metric) Query() string {
	return m.config.Query
}

func (m *Metric) StackdriverData(ctx context.Context, lastPoint time.Time, _ record.MetricRecord) (*metricpb.MetricDescriptor, []*monitoringpb.TimeSeries, error) {
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     m.config.Endpoint,
		Username: m.config.Auth.Username,
		Password: m.config.Auth.Password,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create InfluxDB client: %v", err)
	}

	defer c.Close()

	// We query from [startTime, endTime), where startTime is the timestamp
	// of the latest point, and endTime is the current time with an offset back
	// as points that are too fresh may contain incomplete data.
	startTime := lastPoint
	if !m.config.TimeAggregated {
		// Add a nanosecond offset as for non-time aggregated points, InfluxQL
		// timestamps are inclusive.
		startTime = startTime.Add(time.Nanosecond)
	}
	endTime := timeNow().Add(-m.offsetDuration)
	resp, err := c.Query(m.buildQuery(startTime, endTime))
	if err != nil {
		return nil, nil, err
	} else if err = resp.Error(); err != nil {
		return nil, nil, err
	}

	if len(resp.Results) != 1 {
		return nil, nil, fmt.Errorf("InfluxDB query '%s' returned %d query results, expected 1", m.config.Query, len(resp.Results))
	}

	if len(resp.Results[0].Series) == 0 {
		log.Infof(ctx, "InfluxDB query '%s' returned no time series", m.config.Query)
		return nil, nil, nil
	} else if len(resp.Results[0].Series) > 1 {
		return nil, nil, fmt.Errorf("InfluxDB query '%s' returned %d time series", m.config.Query, len(resp.Results[0].Series))
	}

	series := resp.Results[0].Series[0]
	points, err := parseSeriesPoints(series)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse InfluxDB points: %v", err)
	}

	points, err = m.filterPoints(points, endTime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to filter InfluxDB points: %v", err)
	}

	timeSeries, err := m.convertTimeSeries(points)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert InfluxDB points %v to time series: %v", points, err)
	}

	return m.metricDescriptor(), timeSeries, nil
}

// buildQuery creates a InfluxDB query from the metric query definition,
// wrapped in the given time-interval, inclusive of start time.
func (m *Metric) buildQuery(startTime, endTime time.Time) client.Query {
	query := fmt.Sprintf(
		"SELECT * FROM (%s) WHERE time >= %d AND time < %d",
		m.config.Query,
		startTime.UnixNano(),
		endTime.UnixNano())

	return client.NewQuery(query, m.config.Database, "ns")
}

func (m *Metric) metricDescriptor() *metricpb.MetricDescriptor {
	return &metricpb.MetricDescriptor{
		Type:        m.StackdriverName(),
		MetricKind:  metricpb.MetricDescriptor_GAUGE,
		ValueType:   metricpb.MetricDescriptor_DOUBLE,
		Description: fmt.Sprintf("InfluxDB query: %s", m.Name),
		DisplayName: m.config.Query,
	}
}

func (m *Metric) filterPoints(points []point, endTime time.Time) ([]point, error) {
	if !m.config.TimeAggregated {
		// For non-time-aggregated gauge metrics, we don't need to apply any
		// filtering since we only query InfluxDB for new points, and points
		// that are too fresh are ignored by applying an offset.
		return points, nil
	}

	queryInterval, err := m.config.queryInterval()
	if err != nil {
		return nil, err
	}

	var filteredPoints []point
	for _, p := range points {
		// Once cumulative metrics come in play, we'll filter using lastPoint.
		// For now we can rely on our constructed query to only fetch the
		// latest points.

		// For time-aggregated InfluxQL queries with interval i, Influx returns
		// points with timestamp x, denoting the aggregated value from time
		// [i, i + x).
		intervalEnd := p.timestamp.Add(queryInterval)

		// If this time interval hasn't finished accumulating data, wait for
		// it to complete next time around.
		if intervalEnd.UnixNano() > endTime.UnixNano() {
			continue
		}

		// Update the timestamp on the point as this will be used as the
		// end timestamp in the corresponding Stackdriver time series.
		p.timestamp = intervalEnd
		filteredPoints = append(filteredPoints, p)
	}

	return filteredPoints, nil
}

func (m *Metric) convertTimeSeries(points []point) ([]*monitoringpb.TimeSeries, error) {
	ts := make([]*monitoringpb.TimeSeries, 0, len(points))
	for _, p := range points {
		newP, err := m.convertPoint(p)
		if err != nil {
			return nil, err
		}

		ts = append(ts, &monitoringpb.TimeSeries{
			Metric:     &metricpb.Metric{Type: m.StackdriverName()},
			Resource:   &monitoredres.MonitoredResource{Type: "global"},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			ValueType:  metricpb.MetricDescriptor_DOUBLE,
			Points:     []*monitoringpb.Point{newP},
		})
	}
	return ts, nil
}

// convertPoint converts a parsed InfluxDB point into a Stackdriver point.
func (m *Metric) convertPoint(p point) (*monitoringpb.Point, error) {
	et, err := ptypes.TimestampProto(p.timestamp)
	if err != nil {
		return nil, err
	}

	return &monitoringpb.Point{
		Interval: &monitoringpb.TimeInterval{
			EndTime: et,
		},
		Value: &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_DoubleValue{
				DoubleValue: p.value,
			},
		},
	}, nil
}

type point struct {
	timestamp time.Time
	value     float64
}

// parseSeriesPoints parses points from an InfluxDB series into a slice of
// timestamp-value pairs.
func parseSeriesPoints(series models.Row) ([]point, error) {
	if len(series.Columns) != 2 {
		return nil, fmt.Errorf("series has columns %s, expected only 2 columns", series.Columns)
	} else if series.Columns[0] != "time" {
		return nil, fmt.Errorf("series has first column '%s', expected 'time'", series.Columns[0])
	}

	var points []point
	for _, p := range series.Values {
		t, ok := p[0].(json.Number)
		if !ok {
			return nil, fmt.Errorf("failed to cast %v to json.Number", p[0])
		}
		unixNano, err := t.Int64()
		if err != nil {
			return nil, err
		}

		v, ok := p[1].(json.Number)
		if !ok {
			return nil, fmt.Errorf("failed to cast %v to json.Number", p[1])
		}
		// Since the column types are not specified, we can only assume float64.
		val, err := v.Float64()
		if err != nil {
			return nil, err
		}

		points = append(points, point{time.Unix(0, unixNano), val})
	}

	return points, nil
}
