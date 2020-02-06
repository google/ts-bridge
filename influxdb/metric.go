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
	"strconv"
	"time"

	"github.com/google/ts-bridge/record"

	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/influxdata/influxdb1-client/models"
	client "github.com/influxdata/influxdb1-client/v2"
	"google.golang.org/appengine/log"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// Metric defines a InfluxDB-based metric. It implements the SourceMetric
// interface.
type Metric struct {
	Name                 string
	config               *MetricConfig
	offsetDuration       time.Duration
	counterResetInterval time.Duration
}

// MetricConfig defines the configuration file parameters for a sepcific metric
// imported from InfluxDB.
type MetricConfig struct {
	Query    string
	Database string
	Endpoint string
	Username string
	Password string
}

func NewSourceMetric(name string, config *MetricConfig, offsetDuration, counterResetInterval time.Duration) (*Metric, error) {
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

func (m *Metric) StackdriverData(ctx context.Context, lastPoint time.Time, rec record.MetricRecord) (*metricpb.MetricDescriptor, []*monitoringpb.TimeSeries, error) {
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     m.config.Endpoint,
		Username: m.config.Username,
		Password: m.config.Password,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to create InfluxDB client: %v", err)
	}

	defer c.Close()

	// We query from [startTime, endTime), where startTime is the timestamp
	// of the latest point plus a nanosecond offset as it is inclusive, and
	// endTime is the current time with an offset back as points that are too
	// fresh may contain incomplete data.
	startTime := lastPoint.Add(time.Nanosecond)
	endTime := time.Now().Add(-m.offsetDuration)

	resp, err := c.Query(m.buildQuery(startTime, endTime))
	if err != nil {
		return nil, nil, err
	} else if err = resp.Error(); err != nil {
		return nil, nil, err
	}

	if len(resp.Results) != 1 {
		return nil, nil, fmt.Errorf("InfluxDB query '%s' returned %d query results, expected 1", m.config.Query)
	}

	if len(resp.Results[0].Series) == 0 {
		log.Infof(ctx, "InfluxDB query '%s' returned no time series", m.config.Query)
		return nil, nil, nil
	} else if len(resp.Results[0].Series) > 1 {
		return nil, nil, fmt.Errorf("InfluxDB query '%s' returned %d time series", m.config.Query, len(resp.Results[0].Series))
	}

	serie := resp.Results[0].Series[0]
	points, err := parseSeriePoints(serie)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to parse InfluxDB points: %v", err)
	}

	// For gauge metrics, we don't need to apply any filtering since we only
	// query InfluxDB for new points, and points that are too fresh are ignored
	// by applying an offset. We'll need to apply filtering once cumulative
	// metrics come into play.
	return m.metricDescriptor(), m.convertTimeSeries(points), nil
}

// buildQuery creates a InfluxDB query from the metric query definition,
// wrapped in the given time-interval, inclusive of start time.
func (m *Metric) buildQuery(startTime, endTime time.Time) client.Query {
	query := fmt.Sprintf(
		"SELECT * FROM (%s) WHERE time >= %s AND time < %s",
		m.config.Query,
		strconv.FormatInt(startTime.UnixNano(), 10),
		strconv.FormatInt(endTime.UnixNano(), 10))

	return client.NewQuery(query, m.config.Database, "ns")
}

func (m *Metric) metricDescriptor() *metricpb.MetricDescriptor {
	return &metricpb.MetricDescriptor{
		Type:        m.StackdriverName(),
		MetricKind:  metricpb.MetricDescriptor_GAUGE,
		ValueType:   metricpb.MetricDescriptor_DOUBLE,
		Description: fmt.Sprintf("InfluxDB query: %s", m.config.Query),
		DisplayName: m.Name,
	}
}

func (m *Metric) convertTimeSeries(points []point) []*monitoringpb.TimeSeries {
	ts := make([]*monitoringpb.TimeSeries, 0, len(points))
	for _, p := range points {
		ts = append(ts, &monitoringpb.TimeSeries{
			Metric:     &metricpb.Metric{Type: m.StackdriverName()},
			Resource:   &monitoredres.MonitoredResource{Type: "global"},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			ValueType:  metricpb.MetricDescriptor_DOUBLE,
			Points:     []*monitoringpb.Point{p.convertPoint()},
		})
	}
	return ts
}

type point struct {
	timestamp time.Time
	value     float64
}

// parseSeriePoints parses points from an InfluxDB series into a slice of
// timestamp-value pairs.
func parseSeriePoints(serie models.Row) ([]point, error) {
	if len(serie.Columns) != 2 {
		return nil, fmt.Errorf("Serie has columns %s, expected only 2 columns", serie.Columns)
	} else if serie.Columns[0] != "time" {
		return nil, fmt.Errorf("Serie has first column '%s', expected 'time'", serie.Columns[0])
	}

	var points []point
	for _, p := range serie.Values {
		t, ok := p[0].(json.Number)
		if !ok {
			return nil, fmt.Errorf("Failed to cast %v to json.Number", p[0])
		}
		unixNano, err := t.Int64()
		if err != nil {
			return nil, err
		}

		v, ok := p[1].(json.Number)
		if !ok {
			return nil, fmt.Errorf("Failed to cast %v to json.Number", p[1])
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

// convertPoint converts a parsed InfluxDB point into a Stackdriver point.
func (p *point) convertPoint() *monitoringpb.Point {
	// For gauge metrics without time aggregations, we can treat the timestamps
	// given by Influx as EndTime for the Stackdriver point.
	return &monitoringpb.Point{
		Interval: &monitoringpb.TimeInterval{
			EndTime: p.protoTimestamp(),
		},
		Value: &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_DoubleValue{
				DoubleValue: p.value,
			},
		},
	}
}

func (p *point) protoTimestamp() *timestamp.Timestamp {
	unixNano := p.timestamp.UnixNano()

	return &timestamp.Timestamp{
		Seconds: unixNano / 1e9,
		Nanos:   int32(unixNano % (unixNano / 1e9)),
	}
}
