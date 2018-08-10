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

// Package tsbridge deals with Time Series Bridge configuration files and metric representations.
// This file describes metrics themselves and metric update process.
package tsbridge

import (
	"context"
	"fmt"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"google.golang.org/appengine/log"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// Metric defines a specific metric that will be regularly imported.
type Metric struct {
	Name      string
	Source    SourceMetric
	SDProject string
	Record    *MetricRecord
}

//go:generate mockgen -destination=../mocks/mock_source_metric.go -package=mocks github.com/google/ts-bridge/tsbridge SourceMetric

// SourceMetric is the interface implemented by the source metric libraries (i.e. Datadog).
type SourceMetric interface {
	StackdriverName() string
	Query() string
	StackdriverData(ctx context.Context, since time.Time) (*metricpb.MetricDescriptor, []*monitoringpb.TimeSeries, error)
}

//go:generate mockgen -destination=../mocks/mock_sd_adapter.go -package=mocks github.com/google/ts-bridge/tsbridge StackdriverAdapter

// StackdriverAdapter is an interface implemented by stackdriver.Adapter.
type StackdriverAdapter interface {
	LatestTimestamp(context.Context, string, string) (time.Time, error)
	CreateTimeseries(context.Context, string, string, *metricpb.MetricDescriptor, []*monitoringpb.TimeSeries) error
	Close() error
}

// UpdateAllMetrics updates all metrics listed in a given config.
func UpdateAllMetrics(ctx context.Context, c *Config, sd StackdriverAdapter, s *StatsCollector) (errors []string) {
	oldestWrite := time.Now()
	defer func(start time.Time) {
		stats.Record(ctx, s.TotalImportLatency.M(int64(time.Since(start)/time.Millisecond)))
		stats.Record(ctx, s.OldestMetricAge.M(int64(time.Since(oldestWrite)/time.Millisecond)))
	}(time.Now())

	for _, m := range c.Metrics() {
		err := m.Update(ctx, sd, s)
		if err != nil {
			errors = append(errors, err.Error())
		}
		if m.Record.LastUpdate.Before(oldestWrite) {
			oldestWrite = m.Record.LastUpdate
		}
	}
	return errors
}

// NewMetric creates a Metric based on a SourceMetric and the destination Stackdriver project.
func NewMetric(ctx context.Context, name string, s SourceMetric, sdProject string) (*Metric, error) {
	m := &Metric{
		Name:      name,
		Source:    s,
		SDProject: sdProject,
		Record:    &MetricRecord{Name: name},
	}
	if err := m.Record.load(ctx); err != nil {
		return nil, err
	}
	m.Record.Query = s.Query()
	return m, nil
}

// Update issues a configured query and imports new points to Stackdriver.
func (m *Metric) Update(ctx context.Context, sd StackdriverAdapter, s *StatsCollector) error {
	ctx, err := tag.New(ctx, tag.Insert(s.MetricKey, m.Name))
	if err != nil {
		return err
	}

	start := time.Now()
	defer func(start time.Time) {
		stats.Record(ctx, s.MetricImportLatency.M(int64(time.Since(start)/time.Millisecond)))
	}(start)

	latest, err := sd.LatestTimestamp(ctx, m.SDProject, m.Source.StackdriverName())
	if err != nil {
		if err = m.RecordError(ctx, fmt.Errorf("failed to get latest timestamp: %v", err)); err != nil {
			return err
		}
		return nil
	}

	desc, ts, err := m.Source.StackdriverData(ctx, latest)
	if err != nil {
		if err = m.RecordError(ctx, fmt.Errorf("failed to get data: %v", err)); err != nil {
			return err
		}
		return nil
	}
	if len(ts) > 0 {
		if err = sd.CreateTimeseries(ctx, m.SDProject, m.Source.StackdriverName(), desc, ts); err != nil {
			if err = m.RecordError(ctx, fmt.Errorf("failed to write to Stackdriver: %v", err)); err != nil {
				return err
			}
			return nil
		}
	}
	return m.RecordSuccess(ctx, len(ts), fmt.Sprintf("%d new points found since %v [took %s]", len(ts), latest, time.Since(start)))
}

// RecordError updates metric status in Datastore with a given error message.
func (m *Metric) RecordError(ctx context.Context, e error) error {
	log.Errorf(ctx, "%s: %s", m.Name, e)
	m.Record.LastStatus = fmt.Sprintf("ERROR: %s", e)
	m.Record.LastAttempt = time.Now()
	return m.Record.write(ctx)
}

// RecordSuccess updates metric status in Datastore with a given message.
func (m *Metric) RecordSuccess(ctx context.Context, points int, msg string) error {
	log.Infof(ctx, "%s: %s", m.Name, msg)
	m.Record.LastStatus = fmt.Sprintf("OK: %s", msg)
	m.Record.LastAttempt = time.Now()
	if points > 0 {
		m.Record.LastUpdate = time.Now()
	}
	return m.Record.write(ctx)
}
