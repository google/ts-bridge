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

package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/ts-bridge/record"
	"github.com/google/ts-bridge/stackdriver"
	"github.com/google/ts-bridge/tsbridge"

	"github.com/dustin/go-humanize"
	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
)

func main() {
	http.HandleFunc("/", index)
	http.HandleFunc("/sync", sync)
	http.HandleFunc("/cleanup", cleanup)
	appengine.Main()
}

// sync updates all configured metrics. It's triggered by App Engine Cron.
func sync(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

	t, err := time.ParseDuration(os.Getenv("UPDATE_TIMEOUT"))
	if err != nil {
		logAndReturnError(ctx, w, fmt.Errorf("Could not parse UPDATE_TIMEOUT duration: %v", err))
		return
	}
	ctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()

	if !appengine.IsDevAppServer() && r.Header.Get("X-Appengine-Cron") != "true" {
		http.Error(w, "Only cron requests are allowed here", http.StatusUnauthorized)
		return
	}

	config, err := newConfig(ctx)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}

	sd, err := stackdriver.NewAdapter(ctx)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	defer sd.Close()

	stats, err := tsbridge.NewCollector(ctx)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	defer stats.Close()

	p, err := strconv.Atoi(os.Getenv("UPDATE_PARALLELISM"))
	if err != nil {
		logAndReturnError(ctx, w, fmt.Errorf("could not parse UPDATE_PARALLELISM: %v", err))
		return
	}
	if p < 1 || p > 100 {
		logAndReturnError(ctx, w, fmt.Errorf("expected UPDATE_PARALLELISM between 1 and 100; got %d", p))
		return
	}

	if errs := tsbridge.UpdateAllMetrics(ctx, config, sd, p, stats); errs != nil {
		msg := strings.Join(errs, "; ")
		logAndReturnError(ctx, w, errors.New(msg))
		return
	}
}

// cleanup removes obsolete metric records. It is triggered by App Engine Cron.
func cleanup(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

	if !appengine.IsDevAppServer() && r.Header.Get("X-Appengine-Cron") != "true" {
		http.Error(w, "Only cron requests are allowed here", http.StatusUnauthorized)
		return
	}

	config, err := newConfig(ctx)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}

	var metricNames []string
	for _, m := range config.Metrics() {
		metricNames = append(metricNames, m.Name)
	}
	if err := record.CleanupRecords(ctx, metricNames); err != nil {
		logAndReturnError(ctx, w, err)
	}
}

// index shows a web page with metric import status.
func index(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ENABLE_STATUS_PAGE") != "yes" {
		http.Error(w, "Status page is disabled. Please set ENABLE_STATUS_PAGE to 'yes' to enable it.", http.StatusNotFound)
		return
	}

	ctx := appengine.NewContext(r)
	config, err := newConfig(ctx)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}

	funcMap := template.FuncMap{"humantime": humanize.Time}
	t, err := template.New("index.html").Funcs(funcMap).ParseFiles("index.html")
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	if err := t.Execute(w, config.Metrics()); err != nil {
		logAndReturnError(ctx, w, err)
	}
}

// newConfig initializes and returns tsbridge config.
func newConfig(ctx context.Context) (*tsbridge.Config, error) {
	// TODO: update the variable names to make them not unique to Datadog.
	// This would require documentation update as well for README.md.
	minPointAge, err := time.ParseDuration(os.Getenv("DATADOG_MIN_POINT_AGE"))
	if err != nil {
		return nil, fmt.Errorf("Could not parse DATADOG_MIN_POINT_AGE: %v", err)
	}

	resetInterval, err := time.ParseDuration(os.Getenv("DATADOG_COUNTER_RESET_INTERVAL"))
	if err != nil {
		return nil, fmt.Errorf("Could not parse DATADOG_COUNTER_RESET_INTERVAL: %v", err)
	}

	return tsbridge.NewConfig(ctx, &tsbridge.ConfigOptions{
		Filename:             os.Getenv("CONFIG_FILE"),
		MinPointAge:          minPointAge,
		CounterResetInterval: resetInterval,
	})
}

// Since some URLs are triggered by App Engine cron, error messages returned in HTTP response
// might not be visible to humans. We need to log them as well, and this helper function does that.
func logAndReturnError(ctx context.Context, w http.ResponseWriter, err error) {
	log.Errorf(ctx, err.Error())
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
