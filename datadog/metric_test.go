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
	"io/ioutil"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"

	log "github.com/sirupsen/logrus"

	"github.com/google/ts-bridge/datastore"
	"github.com/google/ts-bridge/mocks"

	"github.com/golang/mock/gomock"
	"github.com/golang/protobuf/proto"
	ddapi "github.com/zorkian/go-datadog-api"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())
	// Save the emulator's quit channel.
	quit := datastore.Emulator(ctx, true)
	code := m.Run()
	cancel()
	// Wait for channel close before exiting the test suite
	<-quit
	os.Exit(code)
}

// checks that `value` is close to `target` within `margin`.
func timeWithin(value, target time.Time, margin time.Duration) bool {
	return math.Abs(value.Sub(target).Seconds()) <= margin.Seconds()
}

func mustUnmarshalText(s string, pb proto.Message) {
	if err := proto.UnmarshalText(s, pb); err != nil {
		panic(err)
	}
}

// fixtureHandler implements http.Handler
type fixtureHandler struct {
	filename string
}

func (h *fixtureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.filename == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	b, err := ioutil.ReadFile(filepath.Join("testdata", h.filename))
	if err != nil {
		panic(err)
	}
	w.Write(b)
}

func makeTestServer(filename string) (*fixtureHandler, *httptest.Server) {
	mux := http.NewServeMux()
	handler := &fixtureHandler{filename}
	mux.Handle("/api/v1/query", handler)
	server := httptest.NewServer(mux)
	return handler, server
}

func TestStackdriverDataErrors(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

	handler, server := makeTestServer("")
	defer server.Close()
	m, _ := NewSourceMetric("metricname", &MetricConfig{Query: "metricquery"}, time.Second, time.Hour)
	m.client.SetBaseUrl(server.URL)

	// At this point HTTP server returns 404 to all requests, so we might as well test error handling.
	if _, _, err := m.StackdriverData(ctx, time.Now().Add(-time.Minute), &datastore.StoredMetricRecord{Storage: storage}); err == nil {
		t.Error("expected an error when server returns 404")
	}

	// A query needs to return a single time series.
	handler.filename = "multiple_ts.json"
	_, _, err := m.StackdriverData(ctx, time.Now().Add(-time.Minute), &datastore.StoredMetricRecord{Storage: storage})
	if err == nil || !strings.Contains(err.Error(), "returned 2 time series") {
		t.Errorf("expected StackdriverData error to say 'returned 2 time series'; got %v", err)
	}

	// No time series is not an error, however `ts` needs to be a 0-length slice.
	handler.filename = "no_ts.json"
	_, ts, err := m.StackdriverData(ctx, time.Now().Add(-time.Minute), &datastore.StoredMetricRecord{Storage: storage})
	if err != nil {
		t.Errorf("expected no errors when query returns no timeseries'; got %v", err)
	}
	if len(ts) != 0 {
		t.Errorf("expected 0 time series; got %v", ts)
	}
}

func TestStackdriverDataResponses(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

	for _, tt := range []struct {
		filename string
		metric   *MetricConfig
		wantDesc string
		wantTSen int
		wantTS   string
	}{
		{
			"good.json",
			&MetricConfig{Query: "metricquery"},
			`
				type: "custom.googleapis.com/datadog/metricname"
				metric_kind: GAUGE
				value_type: DOUBLE
				unit: "B/s"
				description: "Datadog query: metricquery"
				display_name: "system.net.bytes_rcvd"`,
			8,
			`
				metric: < type: "custom.googleapis.com/datadog/metricname" >
				resource: < type: "global" >
				metric_kind: GAUGE
				value_type: DOUBLE
				points: <
					interval: <
						end_time: < seconds: 1531324638 nanos: 123000064 >
					>
					value: < double_value: 2411.4913024902344 >
				>
			`,
		},
		{
			"good_cumulative.json",
			&MetricConfig{Query: "cumsum(metricquery)", Cumulative: true},
			`
				type: "custom.googleapis.com/datadog/metricname"
				metric_kind: CUMULATIVE
				value_type: DOUBLE
				unit: "pkt"
				description: "Datadog query: cumsum(metricquery)"
				display_name: "cumsum(system.net.packets_in.count)"`,
			3,
			`
				metric: < type: "custom.googleapis.com/datadog/metricname" >
				resource: < type: "global" >
				metric_kind: CUMULATIVE
				value_type: DOUBLE
				points: <
					interval: <
						start_time: < seconds: 1515000001>
						end_time: < seconds: 1542908801>
					>
					value: < double_value: 2617.9245496690273>
				>
			`,
		},
	} {
		t.Run(tt.filename, func(t *testing.T) {
			_, server := makeTestServer(tt.filename)
			defer server.Close()
			m, err := NewSourceMetric("metricname", tt.metric, time.Second, time.Hour*24*365*100)
			if err != nil {
				t.Fatalf("unexpected error from NewSourceMetric: %v", err)
			}
			m.client.SetBaseUrl(server.URL)

			desc, ts, err := m.StackdriverData(ctx, time.Unix(1515000000, 0), &datastore.StoredMetricRecord{Storage: storage})
			if err != nil {
				t.Errorf("expected no errors; got %v", err)
			}
			wantDesc := &metricpb.MetricDescriptor{}
			mustUnmarshalText(tt.wantDesc, wantDesc)
			if !proto.Equal(desc, wantDesc) {
				t.Errorf("expected descriptor %v; got %v", wantDesc, desc)
			}
			if len(ts) != tt.wantTSen {
				t.Errorf("expected %d time series objects; got %d", tt.wantTSen, len(ts))
			}

			wantTS := &monitoringpb.TimeSeries{}
			mustUnmarshalText(tt.wantTS, wantTS)
			if !proto.Equal(ts[0], wantTS) {
				t.Errorf("expected time series %v; got %v", wantTS, ts[0])
			}
		})
	}
}

func TestPointsGetFilteredOut(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

	_, server := makeTestServer("good.json")
	defer server.Close()

	for _, tt := range []struct {
		minPointAge time.Duration
		lastPoint   time.Time
	}{
		// All points in good.json are more fresh than 100 years ago, so they should all be
		// filtered out based on minPointAge.
		{time.Hour * 24 * 365 * 100, time.Now().Add(-time.Hour * 24 * 365 * 100)},
		// All points in good.json are older than current time, so they should all be
		// filtered out based on lastPoint.
		{time.Minute, time.Now()},
	} {
		m, _ := NewSourceMetric("metricname", &MetricConfig{Query: "metricquery"}, tt.minPointAge, time.Hour)
		m.client.SetBaseUrl(server.URL)

		_, ts, err := m.StackdriverData(ctx, tt.lastPoint, &datastore.StoredMetricRecord{Storage: storage})
		if err != nil {
			t.Errorf("expected no errors; got %v", err)
		}
		if len(ts) != 0 {
			t.Errorf("expected 0 time series objects; got %d", len(ts))
		}
	}
}

func TestFilterPoints(t *testing.T) {
	m, _ := NewSourceMetric("metricname", &MetricConfig{Query: "foo"}, time.Minute, time.Hour)
	value := float64(0)
	var points []ddapi.DataPoint
	var timestamps []float64
	for _, secAgo := range []int{30, 90, 180} {
		// datadog timestamps are in milliseconds.
		ts := float64(time.Now().Add(-time.Duration(secAgo)*time.Second).Unix() * 1000)
		points = append(points, ddapi.DataPoint{&ts, &value})
		timestamps = append(timestamps, ts)
	}
	got, err := m.filterPoints(time.Now().Add(-2*time.Minute), points)
	if err != nil {
		t.Errorf("expected no errors; got %v", err)
	}
	// T-30s should be filtered out based on minPointAge of 1 minute.
	// T-90s should remain.
	// T-180s should be filtered out based on lastPoint timestamp that is 2min ago.
	if len(got) != 1 || *got[0][0] != timestamps[1] {
		t.Errorf("expected 1 point with timestamp 90s ago; got %v", got)
	}
}

func TestStackdriverDataUnits(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

	handler, server := makeTestServer("")
	defer server.Close()
	m, _ := NewSourceMetric("metricname", &MetricConfig{Query: "metricquery"}, time.Second, time.Hour)
	m.client.SetBaseUrl(server.URL)

	for _, tt := range []struct {
		filename string
		wantUnit string
	}{
		{"good.json", "B/s"},
		{"single_unit.json", "B"},
		{"no_unit.json", ""},
	} {
		handler.filename = tt.filename
		desc, _, err := m.StackdriverData(ctx, time.Now().Add(-time.Minute), &datastore.StoredMetricRecord{Storage: storage})
		if err != nil {
			t.Errorf("%s: expected no errors; got %v", tt.filename, err)
		}
		if desc.GetUnit() != tt.wantUnit {
			t.Errorf("%s: expected unit '%s'; got '%s'", tt.filename, tt.wantUnit, desc.GetUnit())
		}
	}
}

func TestMetricConfig(t *testing.T) {
	c := &MetricConfig{Query: "foo"}
	m, _ := NewSourceMetric("metricname", c, time.Second, time.Hour)

	q := m.Query()
	if q != "foo" {
		t.Errorf("expected Query() to return the Datadog query; got %v", q)
	}

	_, err := NewSourceMetric("metric2", &MetricConfig{Query: "foo", Cumulative: true}, time.Second, time.Hour)
	if err == nil {
		t.Errorf("expected NewSourceMetric to return error for a cumulative metric without cumsum() function")
	}
}

func TestCounterStartTime(t *testing.T) {
	ctx := context.Background()

	t5minAgo := time.Now().Add(-5 * time.Minute)
	t4min59secAgo := time.Now().Add(-5*time.Minute + time.Second)
	t30minAgo := time.Now().Add(-30 * time.Minute)
	t61minAgo := time.Now().Add(-61 * time.Minute)
	// counter reset interval is 1 hour for these tests.
	for _, tt := range []struct {
		name                    string
		lastCounterStartTime    time.Time
		lastPoint               time.Time
		wantCounterStartTime    time.Time
		wantCounterStartTimeSet int
	}{
		{"counter was reset recently, use existing start time",
			t30minAgo, t5minAgo, t30minAgo, 0},
		{"counter reset 61min ago, needs to be reset",
			t61minAgo, t5minAgo, t4min59secAgo, 1},
		{"counter reset 61min ago, but last point is too old. Needs to be reset to half the reset interval",
			t61minAgo, time.Time{}, t30minAgo, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()
			m := &Metric{counterResetInterval: time.Hour}
			r := mocks.NewMockMetricRecord(mockCtrl)
			r.EXPECT().GetCounterStartTime().AnyTimes().DoAndReturn(func() time.Time { return tt.lastCounterStartTime })
			r.EXPECT().SetCounterStartTime(gomock.Any(), gomock.Any()).Times(tt.wantCounterStartTimeSet).DoAndReturn(
				func(ctx context.Context, start time.Time) error {
					tt.lastCounterStartTime = start
					return nil
				})

			got, err := m.counterStartTime(ctx, tt.lastPoint, r)
			if err != nil {
				t.Errorf("unexpected error while calling counterStartTime: %v", err)
			}

			if !timeWithin(got, tt.wantCounterStartTime, 500*time.Millisecond) {
				t.Errorf("wanted start time around %v; got %v", tt.wantCounterStartTime, got)
			}
		})
	}
}

func benchmarkStackdriverData(filename string, b *testing.B) {
	log.SetLevel(log.WarnLevel)
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})
	_, server := makeTestServer(filename)
	defer server.Close()
	m, _ := NewSourceMetric("metricname", &MetricConfig{Query: "metricquery"}, time.Second, time.Hour)

	m.client.SetBaseUrl(server.URL)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		m.StackdriverData(ctx, time.Unix(1515000000, 0), &datastore.StoredMetricRecord{Storage: storage})
	}
}

func BenchmarkStackdriverData10(b *testing.B)     { benchmarkStackdriverData("10_points.json", b) }
func BenchmarkStackdriverData100(b *testing.B)    { benchmarkStackdriverData("100_points.json", b) }
func BenchmarkStackdriverData1000(b *testing.B)   { benchmarkStackdriverData("1000_points.json", b) }
func BenchmarkStackdriverData10000(b *testing.B)  { benchmarkStackdriverData("10000_points.json", b) }
func BenchmarkStackdriverData100000(b *testing.B) { benchmarkStackdriverData("100000_points.json", b) }
