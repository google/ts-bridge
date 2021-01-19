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
	"net/url"
	"sync"
	"time"

	"github.com/google/ts-bridge/stackdriver"
	"github.com/google/ts-bridge/storage"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// Metric defines a specific metric that will be regularly imported.
type Metric struct {
	Name      string
	Source    SourceMetric
	SDProject string
	Record    storage.MetricRecord
}

// Metrics contains all external metric dependencies
type Metrics struct {
	SDClient       StackdriverAdapter
	StatsCollector *StatsCollector
}

// New returns a Metrics struct.
func New(ctx context.Context, sd *stackdriver.Adapter, sc *StatsCollector) *Metrics {
	return &Metrics{
		SDClient:       sd,
		StatsCollector: sc,
	}
}

//go:generate mockgen -destination=../mocks/mock_source_metric.go -package=mocks github.com/google/ts-bridge/tsbridge SourceMetric

// SourceMetric is the interface implemented by the source metric libraries (i.e. Datadog).
type SourceMetric interface {
	StackdriverName() string
	Query() string
	StackdriverData(ctx context.Context, since time.Time, record storage.MetricRecord) (*metricpb.MetricDescriptor, []*monitoringpb.TimeSeries, error)
}

//go:generate mockgen -destination=../mocks/mock_sd_adapter.go -package=mocks github.com/google/ts-bridge/tsbridge StackdriverAdapter

// StackdriverAdapter is an interface implemented by stackdriver.Adapter.
type StackdriverAdapter interface {
	LatestTimestamp(context.Context, string, string) (time.Time, error)
	CreateTimeseries(context.Context, string, string, *metricpb.MetricDescriptor, []*monitoringpb.TimeSeries) error
	Close() error
}

// UpdateAll updates all metrics listed in a given config.
func (m *Metrics) UpdateAll(ctx context.Context, c *MetricConfig, parallelism int) (errors []string) {
	oldestWrite := time.Now()
	defer func(start time.Time) {
		stats.Record(ctx, m.StatsCollector.TotalImportLatency.M(int64(time.Since(start)/time.Millisecond)))
		stats.Record(ctx, m.StatsCollector.OldestMetricAge.M(int64(time.Since(oldestWrite)/time.Millisecond)))
	}(time.Now())

	errchan := make(chan string, len(c.Metrics()))
	sem := make(chan bool, parallelism)
	var wg sync.WaitGroup

	for _, mc := range c.Metrics() {
		sem <- true
		wg.Add(1)
		go func(metric *Metric) {
			defer wg.Done()
			defer func() { <-sem }()

			err := metric.Update(ctx, m.SDClient, m.StatsCollector)
			if err != nil {
				errchan <- err.Error()
			}
		}(mc)
	}
	wg.Wait()
	close(errchan)

	// After all metrics are updated, find the oldest write timestamp.
	for _, m := range c.Metrics() {
		if m.Record.GetLastUpdate().Before(oldestWrite) {
			oldestWrite = m.Record.GetLastUpdate()
		}
	}
	for err := range errchan {
		errors = append(errors, err)
	}
	return errors
}

// NewMetric creates a Metric based on a SourceMetric and the destination Stackdriver project.
func NewMetric(ctx context.Context, name string, s SourceMetric, sdProject string, storage storage.Manager) (*Metric, error) {
	r, err := storage.NewMetricRecord(ctx, name, s.Query())
	if err != nil {
		return nil, err
	}
	return &Metric{
		Name:      name,
		Source:    s,
		SDProject: sdProject,
		Record:    r,
	}, nil
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
		if err = m.Record.UpdateError(ctx, fmt.Errorf("failed to get latest timestamp: %v", err)); err != nil {
			return err
		}
		return nil
	}

	desc, ts, err := m.Source.StackdriverData(ctx, latest, m.Record)
	if err != nil {
		if err = m.Record.UpdateError(ctx, fmt.Errorf("failed to get data: %v", err)); err != nil {
			return err
		}
		return nil
	}
	if len(ts) > 0 {
		if err = sd.CreateTimeseries(ctx, m.SDProject, m.Source.StackdriverName(), desc, ts); err != nil {
			if err = m.Record.UpdateError(ctx, fmt.Errorf("failed to write to Stackdriver: %v", err)); err != nil {
				return err
			}
			return nil
		}
	}
	return m.Record.UpdateSuccess(ctx, len(ts), fmt.Sprintf("%d new points found since %v [took %s]", len(ts), latest, time.Since(start)))
}

// StackdriverURL returns a Metric Explorer URL for a given metric.
func (m *Metric) StackdriverURL() string {
	const xyChartTpl = `{"dataSets":[{"timeSeriesFilter":{"filter":"metric.type=\"%s\" resource.type=\"global\""}}]}`
	u := url.URL{
		Scheme: "https",
		Host:   "app.google.stackdriver.com",
		Path:   "/metrics-explorer",
	}
	q := u.Query()
	q.Set("project", m.SDProject)
	q.Set("xyChart", fmt.Sprintf(xyChartTpl, m.Source.StackdriverName()))
	u.RawQuery = q.Encode()
	return u.String()
}
