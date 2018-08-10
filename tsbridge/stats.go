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
// This file defines metric stats collector and contains metric definitions.
package tsbridge

import (
	"context"
	"fmt"
	"os"
	"sync"

	sdexporter "contrib.go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"google.golang.org/appengine/log"
)

// Manually configured buckets for a distribution metric measuring latency in milliseconds.
var latencyDistribution = view.Distribution(100, 250, 500, 1000, 2000, 3000, 4000, 5000, 7500, 10000, 15000, 20000, 40000, 60000, 90000, 120000, 300000, 600000)

// statsMu makes sure there's only a single active StatsCollector, since OpenCensus metrics need to be globally defined for the whole process.
// This lock is taken when a new collector is created, and released when it's closed.
var statsMu = &sync.Mutex{}

// statsExporter is implemented by Stackdriver exporter and by fakeExporter we use in tests.
type statsExporter interface {
	view.Exporter
	Flush()
}

// StatsCollector has all metrics, tags, and the exporter used to publish them.
type StatsCollector struct {
	Exporter            statsExporter
	MetricImportLatency *stats.Int64Measure
	TotalImportLatency  *stats.Int64Measure
	OldestMetricAge     *stats.Int64Measure
	MetricKey           tag.Key
	views               []*view.View
	ctx                 context.Context
}

// NewCollector creates a new StatsCollector.
// Users need to call StatsCollector.Close() when it's no longer needed. Only a single collector can be active per process.
func NewCollector(ctx context.Context) (*StatsCollector, error) {
	var err error
	c := &StatsCollector{ctx: ctx}

	project := os.Getenv("SD_PROJECT_FOR_INTERNAL_METRICS")
	if project == "" {
		project = appIDFunc(ctx)
	}
	if project == "" {
		return nil, fmt.Errorf("Please define SD_PROJECT_FOR_INTERNAL_METRICS if not running on App Engine")
	}

	c.Exporter, err = sdexporter.NewExporter(sdexporter.Options{
		ProjectID: project,
		OnError:   c.logError,
		Context:   ctx,
	})
	if err != nil {
		return nil, err
	}
	if err = c.registerAndCreateMetrics(); err != nil {
		// Clean up after registerAndCreateMetrics. Don't delete this!
		c.Close()
		return nil, err
	}
	return c, nil
}

// logError is configured as the error handler for Stackdriver exporter.
func (c *StatsCollector) logError(err error) {
	log.Errorf(c.ctx, "StatsCollector: %v", err)
}

// Close unregisters all metrics and Stackdriver exporter and flushes accumulated
// metric points to Stackdriver.
func (c *StatsCollector) Close() {
	view.Unregister(c.views...)
	view.UnregisterExporter(c.Exporter)
	c.Exporter.Flush()
	statsMu.Unlock()
}

// registerAndCreateMetrics registers Stackdriver exporter and metric views.
func (c *StatsCollector) registerAndCreateMetrics() error {
	statsMu.Lock()
	var err error
	view.RegisterExporter(c.Exporter)

	c.MetricKey, err = tag.NewKey("metric_name")
	if err != nil {
		return err
	}

	c.MetricImportLatency = stats.Int64("ts_bridge/metric_import_latencies", "time since last successful import for a metric", stats.UnitMilliseconds)
	c.TotalImportLatency = stats.Int64("ts_bridge/import_latencies", "total time it took to import all metrics", stats.UnitMilliseconds)
	c.OldestMetricAge = stats.Int64("ts_bridge/oldest_metric_age", "oldest time since last successful import across all metrics", stats.UnitMilliseconds)
	c.views = []*view.View{
		&view.View{
			Name:        c.MetricImportLatency.Name(),
			Description: c.MetricImportLatency.Description(),
			Measure:     c.MetricImportLatency,
			Aggregation: latencyDistribution,
			TagKeys:     []tag.Key{c.MetricKey},
		},
		&view.View{
			Name:        c.TotalImportLatency.Name(),
			Description: c.TotalImportLatency.Description(),
			Measure:     c.TotalImportLatency,
			Aggregation: latencyDistribution,
		},
		&view.View{
			Name:        c.OldestMetricAge.Name(),
			Description: c.OldestMetricAge.Description(),
			Measure:     c.OldestMetricAge,
			Aggregation: view.LastValue(),
		},
	}
	if err := view.Register(c.views...); err != nil {
		return err
	}
	return nil
}
