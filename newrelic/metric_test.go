// Copyright 2019 Google LLC
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

package newrelic

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/ts-bridge/record"
	"google.golang.org/appengine/aetest"
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
	mux.Handle("/v2/applications/", handler)
	server := httptest.NewServer(mux)
	return handler, server
}

func TestStackdriverDataErrors(t *testing.T) {
	handler, server := makeTestServer("")
	defer server.Close()
	m, _ := NewSourceMetric("metricname", &MetricConfig{MetricData: MetricData{MetricName: "HttpDispatcher", MetricValue: "total_call_time_per_minute"}, EndpointBase: "https://api.newrelic.com", EndpointPath: "/v2/applications/1234567/metrics/data.json"})
	m.client.BaseUrl = server.URL

	// At this point HTTP server returns 404 to all requests, so we might as well test error handling.
	_, _, err := m.StackdriverData(testCtx, time.Now().Add(-time.Minute), &record.DatastoreMetricRecord{})
	if err == nil {
		t.Error("expected an error when server returns 404")
	}

	// A query needs to return a single time series.
	handler.filename = "datapoint.json"
	_, series, err := m.StackdriverData(testCtx, time.Now().Add(-time.Minute), &record.DatastoreMetricRecord{})
	if err != nil {
		t.Errorf("expected StackdriverData to return 1 timeseries with 1 data point, got error %v", err)
	} else if len(series) != 1 || len(series[0].Points) != 1 {
		t.Errorf("expected StackdriverData to receive 1 timeseries with 1 data point - received %d series.", len(series))
	}
}
