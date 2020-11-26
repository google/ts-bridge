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
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/ts-bridge/env"

	pmexporter "contrib.go.opencensus.io/exporter/prometheus"
	sdexporter "contrib.go.opencensus.io/exporter/stackdriver"
	log "github.com/sirupsen/logrus"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
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

// prometheusExporter is implemented by Prometheus exporter.
type prometheusExporter interface {
	view.Exporter
	http.Handler
}

// StatsCollector has all metrics, tags, and the exporter used to publish them.
type StatsCollector struct {
	// SDExporter is the Stackdriver Exporter.
	SDExporter statsExporter
	// PromExporter is the Prometheus Exporter.
	PromExporter prometheusExporter

	MetricImportLatency *stats.Int64Measure
	TotalImportLatency  *stats.Int64Measure
	OldestMetricAge     *stats.Int64Measure
	MetricKey           tag.Key
	views               []*view.View
	ctx                 context.Context
}

// NewCollector creates a new StatsCollector.
// Users need to call StatsCollector.Close() when it's no longer needed. Only a single collector can be active per process.
func NewCollector(ctx context.Context, project string, backends []string) (*StatsCollector, error) {
	var err error
	c := &StatsCollector{ctx: ctx}

	if project == "" {
		if env.IsAppEngine() {
			project = os.Getenv("GOOGLE_CLOUD_PROJECT")
			log.Infof("Cannot determine project to store stats in, defaulting to GAE project: %v", project)
		} else {
			return nil, fmt.Errorf("error initializing stats collector - project empty: set SD_PROJECT_FOR_INTERNAL_METRICS or --stats-sd-project if not running on App Engine")
		}

	}

	for _, b := range backends {
		switch b {
		case "stackdriver":
			c.SDExporter, err = sdexporter.NewExporter(sdexporter.Options{
				ProjectID: project,
				OnError:   c.logError,
				Context:   ctx,
			})
			if err != nil {
				return nil, err
			}
			view.RegisterExporter(c.SDExporter)

		case "prometheus":
			c.PromExporter, err = pmexporter.NewExporter(pmexporter.Options{
				// Namespace regexs much match ^[a-zA-Z_:][a-zA-Z0-9_:]*$
				Namespace: strings.ReplaceAll(project, "-", "_"),
				OnError:   c.logError,
			})
			if err != nil {
				return nil, err
			}
			view.RegisterExporter(c.PromExporter)
			http.HandleFunc("/metrics", c.PromExporter.ServeHTTP)

		default:
			return nil, fmt.Errorf("Unknown monitoring backend %v", b)
		}

	}

	if err := c.registerAndCreateMetrics(); err != nil {
		// Clean up after registerAndCreateMetrics. Don't delete this!
		c.Close()
		return nil, err
	}
	return c, nil
}

// logError is configured as the error handler for Opencensus exporter(s).
func (c *StatsCollector) logError(err error) {
	log.WithContext(c.ctx).Errorf("StatsCollector: %v", err)
}

// Close unregisters all metrics and Opencensus exporter(s) and flushes
// accumulated metric points to Stackdriver.
func (c *StatsCollector) Close() {
	view.Unregister(c.views...)
	if c.SDExporter != nil {
		view.UnregisterExporter(c.SDExporter)
		c.SDExporter.Flush()
	}
	if c.PromExporter != nil {
		view.UnregisterExporter(c.PromExporter)
	}
	statsMu.Unlock()
}

// registerAndCreateMetrics registers Opencensus exporter(s) and metric views.
func (c *StatsCollector) registerAndCreateMetrics() error {
	statsMu.Lock()
	var err error

	// Reporting period is set very high here to effectively disable regular flushing of metrics by OpenCensus
	// view worker. Since stats collector is relatively short-lived, we rely on metric flushing that happens when
	// the exporter is closed by the Close function above (at the end of each sync operation).
	view.SetReportingPeriod(time.Hour)

	c.MetricKey, err = tag.NewKey("metric_name")
	if err != nil {
		return err
	}

	c.MetricImportLatency = stats.Int64("ts_bridge/metric_import_latencies", "time since last successful import for a metric", stats.UnitMilliseconds)
	c.TotalImportLatency = stats.Int64("ts_bridge/import_latencies", "total time it took to import all metrics", stats.UnitMilliseconds)
	c.OldestMetricAge = stats.Int64("ts_bridge/oldest_metric_age", "oldest time since last successful import across all metrics", stats.UnitMilliseconds)
	c.views = []*view.View{
		{
			Name:        c.MetricImportLatency.Name(),
			Description: c.MetricImportLatency.Description(),
			Measure:     c.MetricImportLatency,
			Aggregation: latencyDistribution,
			TagKeys:     []tag.Key{c.MetricKey},
		},
		{
			Name:        c.TotalImportLatency.Name(),
			Description: c.TotalImportLatency.Description(),
			Measure:     c.TotalImportLatency,
			Aggregation: latencyDistribution,
		},
		{
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
