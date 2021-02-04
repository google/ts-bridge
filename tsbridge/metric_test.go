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

package tsbridge

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/ts-bridge/datastore"
	"github.com/google/ts-bridge/mocks"

	"github.com/golang/mock/gomock"
	"go.opencensus.io/stats/view"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// checks that `value` is close to `target` within `margin`.
func durationWithin(value, target, margin time.Duration) bool {
	return math.Abs(value.Seconds()-target.Seconds()) <= margin.Seconds()
}

type fakeExporter struct {
	values map[string]view.AggregationData
}

func (e *fakeExporter) ExportView(d *view.Data) {
	for _, r := range d.Rows {
		var metricWithTags bytes.Buffer
		metricWithTags.WriteString(d.View.Name)
		for _, t := range r.Tags {
			metricWithTags.WriteString(fmt.Sprintf(":%v", t.Value))
		}
		e.values[metricWithTags.String()] = r.Data
	}
}
func (e *fakeExporter) Flush() {}
func fakeStats(t *testing.T) (*StatsCollector, *fakeExporter) {
	e := &fakeExporter{values: make(map[string]view.AggregationData)}
	c := &StatsCollector{SDExporter: e}
	view.RegisterExporter(c.SDExporter)
	if err := c.registerAndCreateMetrics(); err != nil {
		t.Fatalf("Cannot initialize collector: %v", err)
	}
	return c, e
}

var metricUpdateTests = []struct {
	name       string
	setup      func(*mocks.MockSourceMetric, *mocks.MockStackdriverAdapter)
	wantStatus string
}{
	{"error getting timestamp", func(src *mocks.MockSourceMetric, sd *mocks.MockStackdriverAdapter) {
		// Update fails if we can't get latest timestamp from Stackdriver.
		sd.EXPECT().LatestTimestamp(gomock.Any(), "sd-project", "sd-metricname").Return(
			time.Time{}, fmt.Errorf("some-error"))
	}, "failed to get latest timestamp: some-error"},

	{"error getting new data", func(src *mocks.MockSourceMetric, sd *mocks.MockStackdriverAdapter) {
		// Update fails when we can't get fresh data from the source (e.g. Datadog).
		// This also verifies that `latest` is propagated correctly.
		latest := time.Now().Add(-5 * time.Minute)
		sd.EXPECT().LatestTimestamp(gomock.Any(), "sd-project", "sd-metricname").Return(latest, nil)
		src.EXPECT().StackdriverData(gomock.Any(), latest, gomock.Any()).Return(nil, nil, fmt.Errorf("another-error"))
	}, "failed to get data: another-error"},

	{"no new points", func(src *mocks.MockSourceMetric, sd *mocks.MockStackdriverAdapter) {
		// If `StackdriverData` returns no new points, this should be logged. It's not an error.
		latest := time.Now().Add(-5 * time.Minute)
		sd.EXPECT().LatestTimestamp(gomock.Any(), "sd-project", "sd-metricname").Return(latest, nil)
		src.EXPECT().StackdriverData(gomock.Any(), latest, gomock.Any()).Return(nil, nil, nil)
	}, "0 new points found"},

	{"error writing to stackdriver", func(src *mocks.MockSourceMetric, sd *mocks.MockStackdriverAdapter) {
		// In this case everything happens successfully up until we try to write data to Stackdriver.
		latest := time.Now().Add(-5 * time.Minute)
		sd.EXPECT().LatestTimestamp(gomock.Any(), "sd-project", "sd-metricname").Return(latest, nil)

		descr := &metricpb.MetricDescriptor{Description: "foobar"}
		ts := []*monitoringpb.TimeSeries{{ValueType: metricpb.MetricDescriptor_DOUBLE}}
		src.EXPECT().StackdriverData(gomock.Any(), latest, gomock.Any()).Return(descr, ts, nil)
		sd.EXPECT().CreateTimeseries(gomock.Any(), "sd-project", "sd-metricname", descr, ts).Return(
			fmt.Errorf("some-error"))
	}, "failed to write to Stackdriver: some-error"},

	{"success", func(src *mocks.MockSourceMetric, sd *mocks.MockStackdriverAdapter) {
		latest := time.Now().Add(-5 * time.Minute)
		sd.EXPECT().LatestTimestamp(gomock.Any(), "sd-project", "sd-metricname").Return(latest, nil)

		descr := &metricpb.MetricDescriptor{Description: "foobar"}
		ts := []*monitoringpb.TimeSeries{{ValueType: metricpb.MetricDescriptor_DOUBLE}}
		src.EXPECT().StackdriverData(gomock.Any(), latest, gomock.Any()).Return(descr, ts, nil)
		sd.EXPECT().CreateTimeseries(gomock.Any(), "sd-project", "sd-metricname", descr, ts).Return(nil)
	}, "1 new points found"},
}

func TestMetricUpdate(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

	for _, tt := range metricUpdateTests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			mockSource := mocks.NewMockSourceMetric(mockCtrl)
			mockSource.EXPECT().Query()
			mockSource.EXPECT().StackdriverName().MaxTimes(100).Return("sd-metricname")

			m, err := NewMetric(ctx, "metricname", mockSource, "sd-project", storage)
			if err != nil {
				t.Fatalf("error while creating metric: %v", err)
			}
			rec := m.Record.(*datastore.StoredMetricRecord)
			rec.LastStatus = "OK: all good"
			rec.LastAttempt = time.Now().Add(-time.Hour)

			mockSD := mocks.NewMockStackdriverAdapter(mockCtrl)
			tt.setup(mockSource, mockSD)

			collector, exporter := fakeStats(t)

			// Any errors during the update are recorded in MetricRecord, so the function itself
			// should succeed in all these cases.
			if err := m.Update(ctx, mockSD, collector); err != nil {
				t.Errorf("Metric.Update() returned error %v", err)
			}
			if time.Now().Sub(rec.LastAttempt) > time.Minute {
				t.Errorf("expected to see LastAttempt updated")
			}
			if !strings.Contains(rec.LastStatus, tt.wantStatus) {
				t.Errorf("expected to see LastStatus contain '%s'; got %s", tt.wantStatus, rec.LastStatus)
			}
			collector.Close()
			if got, ok := exporter.values["ts_bridge/metric_import_latencies:metricname"]; !ok {
				t.Errorf("expected to see import latency recorded; got %v", got)
			}
		})
	}
}

func TestMetricImportLatencyMetric(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockSource := mocks.NewMockSourceMetric(mockCtrl)
	mockSource.EXPECT().Query()
	mockSource.EXPECT().StackdriverName().MaxTimes(100).Return("sd-metricname")

	m, err := NewMetric(ctx, "metricname", mockSource, "sd-project", storage)
	if err != nil {
		t.Fatalf("error while creating metric: %v", err)
	}
	mockSD := mocks.NewMockStackdriverAdapter(mockCtrl)
	mockSD.EXPECT().LatestTimestamp(gomock.Any(), "sd-project", "sd-metricname").DoAndReturn(
		func(ctx context.Context, project, name string) (time.Time, error) {
			time.Sleep(100 * time.Millisecond)
			return time.Now(), fmt.Errorf("some error")
		})

	collector, exporter := fakeStats(t)

	if err := m.Update(ctx, mockSD, collector); err != nil {
		t.Errorf("Metric.Update() returned error %v", err)
	}
	collector.Close()

	val, ok := exporter.values["ts_bridge/metric_import_latencies:metricname"]
	got := time.Duration(val.(*view.DistributionData).Mean) * time.Millisecond
	if !ok || !durationWithin(got, 100*time.Millisecond, 40*time.Millisecond) {
		t.Errorf("expected to see import latency around 100ms; got %v", got)
	}
}

var updateAllMetricsTests = []struct {
	name             string
	parallelism      int
	numMetrics       int
	numPoints        int
	wantTotalLatency time.Duration
	wantOldestAge    time.Duration
}{
	{"1 metric, no points", 1, 1, 0, 100 * time.Millisecond, time.Hour + 100*time.Millisecond},
	{"2 metrics, no points", 1, 2, 0, 200 * time.Millisecond, time.Hour + 200*time.Millisecond},
	{"1 metric, 1 points", 1, 1, 1, 100 * time.Millisecond, 100 * time.Millisecond},
	{"2 metrics, 1 points", 1, 2, 1, 200 * time.Millisecond, 200 * time.Millisecond},
	{"parallelism 5, 5 metrics", 5, 5, 1, 100 * time.Millisecond, 100 * time.Millisecond},
	{"paralellism 5, 10 metrics", 5, 10, 1, 200 * time.Millisecond, 200 * time.Millisecond},
}

func TestUpdateAllMetrics(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

	for _, tt := range updateAllMetricsTests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			config := &MetricConfig{}
			for i := 0; i < tt.numMetrics; i++ {
				name := fmt.Sprintf("metric-%d", i)
				src := mocks.NewMockSourceMetric(mockCtrl)
				var ts []*monitoringpb.TimeSeries
				for j := 0; j < tt.numPoints; j++ {
					ts = append(ts, &monitoringpb.TimeSeries{ValueType: metricpb.MetricDescriptor_DOUBLE})
				}
				src.EXPECT().StackdriverData(gomock.Any(), gomock.Any(), gomock.Any()).Return(
					&metricpb.MetricDescriptor{}, ts, nil)
				src.EXPECT().StackdriverName().MaxTimes(100).Return(name)
				metric := &Metric{
					Name:   name,
					Record: &datastore.StoredMetricRecord{LastUpdate: time.Now().Add(-time.Hour), Storage: storage},
					Source: src,
				}
				config.metrics = append(config.metrics, metric)
			}

			mockSD := mocks.NewMockStackdriverAdapter(mockCtrl)
			// Running LatestTimestamp for each metric takes 100ms. This is where most of latency comes from.
			mockSD.EXPECT().LatestTimestamp(gomock.Any(), gomock.Any(), gomock.Any()).Times(tt.numMetrics).DoAndReturn(
				func(ctx context.Context, project, name string) (time.Time, error) {
					time.Sleep(100 * time.Millisecond)
					return time.Now(), nil
				})
			mockSD.EXPECT().CreateTimeseries(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(
				tt.numMetrics * tt.numPoints).Return(nil)

			collector, exporter := fakeStats(t)
			metrics := &Metrics{
				SDClient:       mockSD,
				StatsCollector: collector,
			}

			if errs := metrics.UpdateAll(ctx, config, tt.parallelism); len(errs) > 0 {
				t.Errorf("UpdateAllMetrics() returned errors: %v", errs)
			}
			collector.Close()

			val, ok := exporter.values["ts_bridge/import_latencies"]
			latency := time.Duration(val.(*view.DistributionData).Mean) * time.Millisecond
			if !ok || !durationWithin(latency, tt.wantTotalLatency, 75*time.Millisecond) {
				t.Errorf("expected to see import latency around %v; got %v", tt.wantTotalLatency, latency)
			}

			val, ok = exporter.values["ts_bridge/oldest_metric_age"]
			age := time.Duration(val.(*view.LastValueData).Value) * time.Millisecond
			if !ok || !durationWithin(age, tt.wantOldestAge, 75*time.Millisecond) {
				t.Errorf("expected oldest metric age around %v; got %v", tt.wantOldestAge, age)
			}
		})
	}
}

func TestUpdateAllMetricsErrors(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	src := mocks.NewMockSourceMetric(mockCtrl)
	src.EXPECT().StackdriverName().MaxTimes(100).Return("sd-name")
	config := &MetricConfig{
		metrics: []*Metric{
			{
				// Having an emoji symbol in metric name should produce an error while defining an OpenCensus tag.
				Name:   "invalid metric name 🥒",
				Record: &datastore.StoredMetricRecord{LastUpdate: time.Now().Add(-time.Hour), Storage: storage},
				Source: src,
			},
		},
	}

	mockSD := mocks.NewMockStackdriverAdapter(mockCtrl)
	collector, _ := fakeStats(t)
	defer collector.Close()
	metrics := &Metrics{
		SDClient:       mockSD,
		StatsCollector: collector,
	}

	errs := metrics.UpdateAll(ctx, config, 1)
	if len(errs) != 1 {
		t.Errorf("expected UpdateAllMetrics to return an error")
	}
}
