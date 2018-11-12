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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	ddapi "github.com/zorkian/go-datadog-api"
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
	handler, server := makeTestServer("")
	defer server.Close()
	m := NewSourceMetric("metricname", &MetricConfig{Query: "metricquery"}, time.Second)
	m.client.SetBaseUrl(server.URL)

	// At this point HTTP server returns 404 to all requests, so we might as well test error handling.
	_, _, err := m.StackdriverData(testCtx, time.Now().Add(-time.Minute))
	if err == nil {
		t.Error("expected an error when server returns 404")
	}

	// A query needs to return a single time series.
	handler.filename = "multiple_ts.json"
	_, _, err = m.StackdriverData(testCtx, time.Now().Add(-time.Minute))
	if err == nil || !strings.Contains(err.Error(), "returned 2 time series") {
		t.Errorf("expected StackdriverData error to say 'returned 2 time series'; got %v", err)
	}

	// No time series is not an error, however `ts` needs to be a 0-length slice.
	handler.filename = "no_ts.json"
	_, ts, err := m.StackdriverData(testCtx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Errorf("expected no errors when query returns no timeseries'; got %v", err)
	}
	if len(ts) != 0 {
		t.Errorf("expected 0 time series; got %v", ts)
	}
}

func TestStackdriverDataResponses(t *testing.T) {
	_, server := makeTestServer("good.json")
	defer server.Close()
	m := NewSourceMetric("metricname", &MetricConfig{Query: "metricquery"}, time.Second)
	m.client.SetBaseUrl(server.URL)

	desc, ts, err := m.StackdriverData(testCtx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Errorf("expected no errors'; got %v", err)
	}
	wantDesc := &metricpb.MetricDescriptor{}
	mustUnmarshalText(`
        type: "custom.googleapis.com/datadog/metricname"
        metric_kind: GAUGE
        value_type: DOUBLE
        unit: "B/s"
        description: "Datadog query: metricquery"
        display_name: "system.net.bytes_rcvd"`, wantDesc)
	if !proto.Equal(desc, wantDesc) {
		t.Errorf("expected descriptor %v; got %v", wantDesc, desc)
	}
	if len(ts) != 8 {
		t.Errorf("expected 8 time series objects; got %d", len(ts))
	}

	wantTS := &monitoringpb.TimeSeries{}
	mustUnmarshalText(`
        metric: < type: "custom.googleapis.com/datadog/metricname" >
        resource: < type: "global" >
        metric_kind: GAUGE
        value_type: DOUBLE
        points: <
            interval: <
                end_time: < seconds: 1531324638 nanos: 123000064 >
            >
            value: < double_value: 2411.4913024902344 >
        >`, wantTS)
	if !proto.Equal(ts[0], wantTS) {
		t.Errorf("expected time series %v; got %v", wantTS, ts[0])
	}
}

func TestNewPointsGetFilteredOut(t *testing.T) {
	_, server := makeTestServer("good.json")
	defer server.Close()
	m := NewSourceMetric("metricname", &MetricConfig{Query: "metricquery"}, time.Hour*24*365*100)
	m.client.SetBaseUrl(server.URL)

	_, ts, err := m.StackdriverData(testCtx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Errorf("expected no errors; got %v", err)
	}
	// All points in good.json are more fresh than 100 years ago, so we should get 0 points back.
	if len(ts) != 0 {
		t.Errorf("expected 0 time series objects; got %d", len(ts))
	}
}

func TestFilterPoints(t *testing.T) {
	m := NewSourceMetric("metricname", &MetricConfig{Query: "foo"}, time.Minute)
	value := float64(0)
	var points []ddapi.DataPoint
	for _, secAgo := range []int{30, 90} {
		// datadog timestamps are in milliseconds.
		ts := float64(time.Now().Add(-time.Duration(secAgo)*time.Second).Unix() * 1000)
		points = append(points, ddapi.DataPoint{&ts, &value})
	}
	got, err := m.filterPoints(points)
	if err != nil {
		t.Errorf("expected no errors; got %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 point; got %v", got)
	}
}

func TestStackdriverDataUnits(t *testing.T) {
	handler, server := makeTestServer("")
	defer server.Close()
	m := NewSourceMetric("metricname", &MetricConfig{Query: "metricquery"}, time.Second)
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
		desc, _, err := m.StackdriverData(testCtx, time.Now().Add(-time.Minute))
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
	m := NewSourceMetric("metricname", c, time.Second)

	q := m.Query()
	if q != "foo" {
		t.Errorf("expected Query() to return the Datadog query; got %v", q)
	}
}
