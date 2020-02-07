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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/ts-bridge/record"

	"github.com/golang/protobuf/proto"
	"google.golang.org/appengine/aetest"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

var testCtx context.Context

func TestMain(m *testing.M) {
	var done func()
	var err error
	testCtx, done, err = aetest.NewContext()
	if err != nil {
		panic(err)
	}

	code := m.Run()
	done()
	os.Exit(code)
}

// fixtureHandler implements http.Handler. TODO: move fixtureHandler and other
// testing functions under a common testing util pkg.
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

// makeTestServer returns a handler and server for responding to InfluxDB
// queries in tests. The server will respond with the JSON file provided
// by the handler.
func makeTestServer(filename string) (*fixtureHandler, *httptest.Server) {
	mux := http.NewServeMux()
	handler := &fixtureHandler{filename}
	mux.Handle("/query", handler)
	server := httptest.NewServer(mux)
	return handler, server
}

func mustUnmarshalText(s string, pb proto.Message) {
	if err := proto.UnmarshalText(s, pb); err != nil {
		panic(err)
	}
}

func mustUnmarshalTimeSeries(descText string, tsTexts ...string) (*metricpb.MetricDescriptor, []*monitoringpb.TimeSeries) {
	desc := &metricpb.MetricDescriptor{}
	mustUnmarshalText(descText, desc)

	ts := make([]*monitoringpb.TimeSeries, 0, len(tsTexts))
	for _, text := range tsTexts {
		p := &monitoringpb.TimeSeries{}
		mustUnmarshalText(text, p)
		ts = append(ts, p)
	}

	return desc, ts
}

func TestMetricConfig(t *testing.T) {
	c := &MetricConfig{Query: "foo"}
	m, err := NewSourceMetric("metricname", c, time.Second, time.Hour)
	if err != nil {
		t.Fatalf("error creating InfluxDB metric source: %v", err)
	}

	stackdriverName := m.StackdriverName()
	if stackdriverName != "custom.googleapis.com/influxdb/metricname" {
		t.Errorf("expected StackdriverName() to return 'custom.googleapis.com/influxdb/metricname'; got %s", stackdriverName)
	}

	query := m.Query()
	if query != "foo" {
		t.Errorf("expected Query() to return the InfluxDB query 'foo'; got %s", query)
	}
}

func getParam(params url.Values, key string) string {
	vals, ok := params[key]
	if !ok || len(vals) == 0 {
		return ""
	}

	return vals[0]
}

func TestStackdriverDataQuery(t *testing.T) {
	now := time.Unix(0, 1000000000000) // (1000s)
	timeNow = func() time.Time { return now }
	lastPoint := time.Unix(0, 800000000000) // (800s)

	for _, tt := range []struct {
		description string
		config      *MetricConfig
		wantQuery   string
	}{
		{
			description: "correct gauge metric query",
			config: &MetricConfig{
				Query:    "foo",
				Database: "bar",
			},
			wantQuery: "SELECT * FROM (foo) WHERE time >= 800000000001 AND time < 999000000000",
		},
		{
			description: "correct time aggregated gauge metric query",
			config: &MetricConfig{
				Query:          "SELECT MEAN(f) FROM foo GROUP BY time(10s)",
				Database:       "bar",
				TimeAggregated: true,
			},
			wantQuery: "SELECT * FROM (SELECT MEAN(f) FROM foo GROUP BY time(10s)) WHERE time >= 800000000000 AND time < 999000000000",
		},
	} {
		t.Run(tt.description, func(t *testing.T) {
			requestHandled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				params := r.URL.Query()

				db := getParam(params, "db")
				if db != "bar" {
					t.Errorf("expected db=bar to be passed in query; got db=%s", db)
				}

				epoch := getParam(params, "epoch")
				if epoch != "ns" {
					t.Errorf("expected epoch=ns to be passed in query; got epoch=%s", epoch)
				}

				q := getParam(params, "q")
				if q != tt.wantQuery {
					t.Errorf("expected q=%s to be passed in query; got q=%s", tt.wantQuery, q)
				}

				requestHandled = true
			}))
			defer server.Close()

			tt.config.Endpoint = server.URL
			m, _ := NewSourceMetric("metricname", tt.config, time.Second, time.Hour)
			m.StackdriverData(testCtx, lastPoint, &record.DatastoreMetricRecord{})
			if !requestHandled {
				t.Fatalf("StackdriverData did not send InfluxDB request")
			}
		})
	}
}

func TestStackdriverDataErrors(t *testing.T) {
	handler, server := makeTestServer("")
	defer server.Close()

	c := &MetricConfig{
		Query:    "metricquery",
		Endpoint: server.URL,
	}
	m, _ := NewSourceMetric("metricname", c, time.Second, time.Hour)

	for _, tt := range []struct {
		description  string
		filename     string
		wantInErr    string
		wantTsLength int
	}{
		{
			description: "error when server returns 404",
			filename:    "",
			wantInErr:   "404",
		},
		{
			description: "error when server returns no query results",
			filename:    "no_query.json",
			wantInErr:   "returned 0 query results, expected 1",
		},
		{
			description: "error when server returns multiple query results",
			filename:    "multiple_query.json",
			wantInErr:   "returned 2 query results, expected 1",
		},
		{
			description: "error when server returns multiple time series",
			filename:    "multiple_ts.json",
			wantInErr:   "returned 2 time series",
		},
		{
			description:  "no SD time series when server returns no time series",
			filename:     "no_ts.json",
			wantTsLength: 0,
		},
		{
			description: "error when server returns series with multiple value columns",
			filename:    "multiple_cols.json",
			wantInErr:   "expected only 2 columns",
		},
	} {
		t.Run(tt.description, func(t *testing.T) {
			handler.filename = tt.filename
			_, ts, err := m.StackdriverData(testCtx, time.Now(), &record.DatastoreMetricRecord{})
			if err != nil {
				if tt.wantInErr == "" {
					t.Fatalf("unexpected StackdriverData error: %v", err)
				} else if !strings.Contains(err.Error(), tt.wantInErr) {
					t.Fatalf("expected StackdriverData error with '%s'; got '%s'", tt.wantInErr, err.Error())
				}
				return
			}

			if tt.wantInErr != "" {
				t.Fatalf("StackdriverData got nil err, want '%s' in err", tt.wantInErr)
			}

			if len(ts) != tt.wantTsLength {
				t.Fatalf("StackdriverData expected %d time series, got %v", tt.wantTsLength, len(ts))
			}
		})
	}
}

func TestStackdriverDataGaugeResponse(t *testing.T) {
	_, server := makeTestServer("good_gauge.json")
	defer server.Close()

	c := &MetricConfig{
		Query:    "metricquery",
		Endpoint: server.URL,
	}
	m, _ := NewSourceMetric("metricname", c, time.Second, time.Hour)
	// The lastTime time passed here is irrelevant, as we stubbed what the
	// query returns.
	desc, ts, err := m.StackdriverData(testCtx, time.Now(), &record.DatastoreMetricRecord{})
	if err != nil {
		t.Fatalf("unexpected StackdriverData error: %v", err)
	}

	expectedDescRaw := `
		type: "custom.googleapis.com/influxdb/metricname"
		metric_kind: GAUGE
		value_type: DOUBLE
		description: "InfluxDB query: metricname"
		display_name: "metricquery"
	`
	expectedTSRaw := []string{
		`
			metric: < type: "custom.googleapis.com/influxdb/metricname" >
			resource: < type: "global" >
			metric_kind: GAUGE
			value_type: DOUBLE
			points: <
				interval: <
					end_time: < seconds: 1579802400 nanos: 0 >
				>
				value: < double_value: 33.1 >
			>
		`,
		`
			metric: < type: "custom.googleapis.com/influxdb/metricname" >
			resource: < type: "global" >
			metric_kind: GAUGE
			value_type: DOUBLE
			points: <
				interval: <
					end_time: < seconds: 1579803000 nanos: 0 >
				>
				value: < double_value: 48.8 >
			>
		`,
	}

	expectedDesc, expectedTS := mustUnmarshalTimeSeries(expectedDescRaw, expectedTSRaw...)
	if !proto.Equal(desc, expectedDesc) {
		t.Errorf("expected descriptor %v; got %v", expectedDesc, desc)
	}

	if len(ts) != len(expectedTS) {
		t.Fatalf("expected %d time series; got %d", len(expectedTS), len(ts))
	}

	for i, p := range ts {
		if !proto.Equal(p, expectedTS[i]) {
			t.Errorf("expected time series %v; got %v", expectedTS[i], p)
		}
	}
}

func TestStackdriverDataTimeAggregatedGaugeResponse(t *testing.T) {
	_, server := makeTestServer("good_timeagg_gauge.json")
	defer server.Close()

	c := &MetricConfig{
		Query:          "SELECT MEAN(f) FROM foo GROUP BY time(10s)",
		Endpoint:       server.URL,
		TimeAggregated: true,
	}
	m, err := NewSourceMetric("metricname", c, time.Second, 0)
	if err != nil {
		t.Fatalf("failed to create metric with config %v: %v", c, err)
	}

	// With offsetDuration set to 0, this will be the endTime used in the
	// Influx query.
	now := time.Unix(0, 1035000000000) // (1035s)
	timeNow = func() time.Time { return now }

	lastPoint := time.Unix(0, 1015000000000) // (1015s)
	desc, ts, err := m.StackdriverData(testCtx, lastPoint, &record.DatastoreMetricRecord{})
	if err != nil {
		t.Fatalf("unexpected StackdriverData error: %v", err)
	}

	expectedDescRaw := `
		type: "custom.googleapis.com/influxdb/metricname"
		metric_kind: GAUGE
		value_type: DOUBLE
		description: "InfluxDB query: metricname"
		display_name: "SELECT MEAN(f) FROM foo GROUP BY time(10s)"
	`

	// With timestamps from (1010, 1020, 1030), we expect the just the
	// last one to be filtered out. While the first interval is incomplete,
	// it won't ever catch up, so we take what we have.
	expectedTSRaw := []string{
		`
			metric: < type: "custom.googleapis.com/influxdb/metricname" >
			resource: < type: "global" >
			metric_kind: GAUGE
			value_type: DOUBLE
			points: <
				interval: <
					end_time: < seconds: 1020 nanos: 0 >
				>
				value: < double_value: 48.8 >
			>
		`,
		`
			metric: < type: "custom.googleapis.com/influxdb/metricname" >
			resource: < type: "global" >
			metric_kind: GAUGE
			value_type: DOUBLE
			points: <
				interval: <
					end_time: < seconds: 1030 nanos: 0 >
				>
				value: < double_value: 59.4 >
			>
		`,
	}

	expectedDesc, expectedTS := mustUnmarshalTimeSeries(expectedDescRaw, expectedTSRaw...)
	if !proto.Equal(desc, expectedDesc) {
		t.Errorf("expected descriptor %v; got %v", expectedDesc, desc)
	}

	if len(ts) != len(expectedTS) {
		t.Fatalf("expected %d time series; got %d", len(expectedTS), len(ts))
	}

	for i, p := range ts {
		if !proto.Equal(p, expectedTS[i]) {
			t.Errorf("expected time series %v; got %v", expectedTS[i], p)
		}
	}
}
