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
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/ts-bridge/boltdb"
	"github.com/google/ts-bridge/datastore"
	"github.com/google/ts-bridge/env"
	"github.com/google/ts-bridge/stackdriver"
	"github.com/google/ts-bridge/storage"
	"github.com/google/ts-bridge/tsbridge"

	"github.com/dustin/go-humanize"
	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	debug = kingpin.Flag("debug", "enable debug mode").Envar("DEBUG").Default("false").Bool()
	port  = kingpin.Flag("port", "ts-bridge server port").Envar("PORT").Default("8080").Int()

	metricConfig = kingpin.Flag(
		"metric-config", "metric configuration file path",
	).Envar("CONFIG_FILE").Default("metrics.yaml").String()

	enableStatusPage = kingpin.Flag(
		"enable-status-page", "enable ts-bridge server status page",
	).Envar("ENABLE_STATUS_PAGE").Default("false").Bool()

	updateTimeout = kingpin.Flag(
		"update-timeout", "total timeout for updating all metrics.",
	).Envar("UPDATE_TIMEOUT").Default("5m").Duration()

	updateParallelism = kingpin.Flag(
		"update-parallelism", "number of metrics to update in parallel",
	).Envar("UPDATE_PARALLELISM").Default("1").Int()

	minPointAge = kingpin.Flag(
		"min-point-age", "minimum age of points to be imported (allows data to settle before import).",
	).Envar("MIN_POINT_AGE").Default("2m").Duration()

	sdLookBackInterval = kingpin.Flag(
		"sd-lookback-interval", "How far to look back while searching for recent data in Stackdriver.",
	).Envar("SD_LOOKBACK_INTERVAL").Default("1h").Duration()

	counterResetInterval = kingpin.Flag(
		"counter-reset-interval", "how often to reset 'start time' to keep the query time window small enough to avoid aggregation.",
	).Envar("COUNTER_RESET_INTERVAL").Default("30m").Duration()

	sdInternalMetricsProject = kingpin.Flag(
		"stats-sd-project", "Stackdriver project for internal ts-bridge metrics",
	).Envar("SD_PROJECT_FOR_INTERNAL_METRICS").String()

	// Storage options
	storageEngine = kingpin.Flag(
		"storage-engine", "storage engine to keep the metrics metadata in",
	).Envar("STORAGE_ENGINE").Default("datastore").String()

	datastoreProject = kingpin.Flag(
		"datastore-project", "GCP Project to use for communicating with Datastore",
	).Envar("DATASTORE_PROJECT").String()

	boltdbPath = kingpin.Flag("boltdb-path", "path to BoltDB store, e.g. /data/bolt.db").Envar("BOLTDB_PATH").String()
)

func main() {
	kingpin.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
		log.Debug("Debug logging enabled...")
	}

	if err := validateFlags(); err != nil {
		log.Fatalf("Invalid flags: %v", err)
	}

	http.HandleFunc("/", index)
	http.HandleFunc("/sync", sync)
	http.HandleFunc("/cleanup", cleanup)

	// Build a connection string, e.g. ":8080"
	conn := net.JoinHostPort("", strconv.Itoa(*port))
	log.Debugf("Connection string: %v", conn)
	if err := http.ListenAndServe(conn, nil); err != nil {
		log.Fatalf("unable to start serving: %v", err)
	}

}

func validateFlags() error {
	// Verify if updateParallelism is within bounds.
	//   Note: bounds have been chosen arbitrarily.
	if *updateParallelism < 1 || *updateParallelism > 100 {
		return fmt.Errorf("expected --update-parallelism|UPDATE_PARALLELISM between 1 and 100; got %d", *updateParallelism)
	}
	return nil
}

// sync updates all configured metrics. It's triggered by App Engine Cron.
func sync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, cancel := context.WithTimeout(ctx, *updateTimeout)
	defer cancel()

	if env.IsAppEngine() && r.Header.Get("X-Appengine-Cron") != "true" {
		http.Error(w, "Only cron requests are allowed here", http.StatusUnauthorized)
		return
	}

	storage, err := loadStorageEngine(ctx)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	defer storage.Close()

	config, err := newRuntimeConfig(ctx, storage)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}

	sd, err := stackdriver.NewAdapter(ctx, *sdLookBackInterval)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	defer sd.Close()

	stats, err := tsbridge.NewCollector(ctx, *sdInternalMetricsProject)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	defer stats.Close()

	if errs := tsbridge.UpdateAllMetrics(ctx, config, sd, *updateParallelism, stats); errs != nil {
		msg := strings.Join(errs, "; ")
		logAndReturnError(ctx, w, errors.New(msg))
		return
	}
}

// cleanup removes obsolete metric records. It is triggered by App Engine Cron.
func cleanup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if env.IsAppEngine() && r.Header.Get("X-Appengine-Cron") != "true" {
		http.Error(w, "Only cron requests are allowed here", http.StatusUnauthorized)
		return
	}

	storage, err := loadStorageEngine(ctx)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	defer storage.Close()

	config, err := newRuntimeConfig(ctx, storage)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}

	var metricNames []string
	for _, m := range config.Metrics() {
		metricNames = append(metricNames, m.Name)
	}

	if err := storage.CleanupRecords(ctx, metricNames); err != nil {
		logAndReturnError(ctx, w, err)
	}
}

// index shows a web page with metric import status.
func index(w http.ResponseWriter, r *http.Request) {
	if *enableStatusPage != true {
		http.Error(w, "Status page is disabled. Please set ENABLE_STATUS_PAGE or --enable-status-page flag to to enable it.",
			http.StatusNotFound)
		return
	}

	ctx := r.Context()

	storage, err := loadStorageEngine(ctx)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	defer storage.Close()

	config, err := newRuntimeConfig(ctx, storage)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}

	funcMap := template.FuncMap{"humantime": humanize.Time}
	t, err := template.New("index.html").Funcs(funcMap).ParseFiles("app/index.html")
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	if err := t.Execute(w, config.Metrics()); err != nil {
		logAndReturnError(ctx, w, err)
	}
}

// newRuntimeConfig initializes and returns tsbridge config
func newRuntimeConfig(ctx context.Context, storage storage.Manager) (*tsbridge.MetricConfig, error) {
	return tsbridge.NewMetricConfig(ctx, &tsbridge.ConfigOptions{
		Filename:             *metricConfig,
		MinPointAge:          *minPointAge,
		CounterResetInterval: *counterResetInterval,
		Storage:              storage,
	})
}

// Since some URLs are triggered by App Engine cron, error messages returned in HTTP response
// might not be visible to humans. We need to log them as well, and this helper function does that.
func logAndReturnError(ctx context.Context, w http.ResponseWriter, err error) {
	log.WithContext(ctx).WithError(err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// Helper function to load the correct storage manager depending on settings
func loadStorageEngine(ctx context.Context) (storage.Manager, error) {
	switch *storageEngine {
	case "datastore":
		datastoreManager := datastore.New(ctx, &datastore.Options{Project: *datastoreProject})
		return datastoreManager, nil
	case "boltdb":
		if env.IsAppEngine() {
			return nil, fmt.Errorf("BoltDB storage is not supported on AppEngine")
		}
		opts := &boltdb.Options{DBPath: *boltdbPath}

		return boltdb.New(opts), nil
	default:
		return nil, fmt.Errorf("unknown storage engine selected: %s", *storageEngine)
	}
}
