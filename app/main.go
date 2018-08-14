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
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/ts-bridge/stackdriver"
	"github.com/google/ts-bridge/tsbridge"

	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
)

func main() {
	http.HandleFunc("/", index)
	http.HandleFunc("/sync", sync)
	appengine.Main()
}

// sync updates all configured metrics. It's triggered by App Engine Task Queues.
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

	config, err := tsbridge.NewConfig(ctx, os.Getenv("CONFIG_FILE"))
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

// Since /sync is usually triggered by App Engine cron, error messages returned in HTTP response
// will not be visible to humans. We need to log them as well, and this helper function does that.
func logAndReturnError(ctx context.Context, w http.ResponseWriter, err error) {
	log.Errorf(ctx, err.Error())
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// index shows a very simple status UI. TODO(b/111250495): implement better UI.
func index(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	metrics, err := tsbridge.ListRecords(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, m := range metrics {
		w.Write([]byte(fmt.Sprintf("%s\n[%s] %s\n\n", m.Name, m.LastAttempt, m.LastStatus)))
	}
}
