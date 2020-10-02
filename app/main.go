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
	"time"

	"github.com/dustin/go-humanize"
	"github.com/google/ts-bridge/boltdb"
	"github.com/google/ts-bridge/datastore"
	"github.com/google/ts-bridge/env"
	"github.com/google/ts-bridge/stackdriver"
	"github.com/google/ts-bridge/storage"
	"github.com/google/ts-bridge/tsbridge"

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

	syncPeriod = kingpin.Flag(
		"sync-period", "How often to sync metrics when running in standalone mode",
	).Envar("SYNC_PERIOD").Default("60s").Duration()

	syncCleanupAfter = kingpin.Flag(
		"sync-cleanup-after", "Run cleanup after X sync loops",
	).Envar("SYNC_CLEANUP_AFTER").Default("100").Int()

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

	config := tsbridge.NewConfig(&tsbridge.ConfigOptions{
		Filename:                 *metricConfig,
		MinPointAge:              *minPointAge,
		CounterResetInterval:     *counterResetInterval,
		SDLookBackInterval:       *sdLookBackInterval,
		SDInternalMetricsProject: *sdInternalMetricsProject,
		UpdateParallelism:        *updateParallelism,
		EnableStatusPage:         *enableStatusPage,
		StorageEngine:            *storageEngine,
		SyncPeriod:               *syncPeriod,
		SyncCleanupAfter:         *syncCleanupAfter,
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		index(w, r, config)
	})
	http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		syncHandler(w, r, config)
	})
	http.HandleFunc("/cleanup", func(w http.ResponseWriter, r *http.Request) {
		cleanupHandler(w, r, config)
	})

	// Run a sync loop for standalone use
	if !env.IsAppEngine() {
		log.Debug("Running outside of appengine, starting up a sync loop...")
		ctx, cancel := context.WithCancel(context.Background())
		count := 0
		go func() {
			defer cancel()
			for {
				select {
				case <-time.After(config.Options.SyncPeriod):
					ctx, cancel := context.WithTimeout(ctx, *updateTimeout)
					log.WithContext(ctx).Debugf("Running sync...")
					if err := sync(ctx, config); err != nil {
						log.WithContext(ctx).Errorf("error running sync() routine: %v", err)
					}
					// perform a cleanup every nth cycle
					if count == config.Options.SyncCleanupAfter {
						log.WithContext(ctx).Debugf("Running cleanup...")
						if err := cleanup(ctx, config); err != nil {
							log.WithContext(ctx).Errorf("error running the cleanup() routine: %v", err)
							return
						}
						count = 0
					}
					count++
					cancel()
				case <-ctx.Done():
					return
				}
			}
		}()
	}

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

// syncHandler is an HTTP wrapper around sync() method that is designed to be triggered by App Engine Cron.
func syncHandler(w http.ResponseWriter, r *http.Request, config *tsbridge.Config) {
	ctx := r.Context()

	ctx, cancel := context.WithTimeout(ctx, *updateTimeout)
	defer cancel()

	if env.IsAppEngine() && r.Header.Get("X-Appengine-Cron") != "true" {
		http.Error(w, "Only cron requests are allowed here", http.StatusUnauthorized)
		return
	}

	if err := sync(ctx, config); err != nil {
		logAndReturnError(ctx, w, err)
	}
}

// sync updates all configured metrics.
func sync(ctx context.Context, config *tsbridge.Config) error {
	store, err := loadStorageEngine(ctx)
	if err != nil {
		return err
	}
	defer store.Close()

	metrics, err := tsbridge.NewMetricConfig(ctx, config, store)
	if err != nil {
		return err
	}

	sd, err := stackdriver.NewAdapter(ctx, config.Options.SDLookBackInterval)
	if err != nil {
		return err
	}
	defer sd.Close()

	stats, err := tsbridge.NewCollector(ctx, config.Options.SDInternalMetricsProject)
	if err != nil {
		return err
	}
	defer stats.Close()

	if errs := tsbridge.UpdateAllMetrics(ctx, metrics, sd, config.Options.UpdateParallelism, stats); errs != nil {
		msg := strings.Join(errs, "; ")
		return errors.New(msg)
	}
	return nil
}

// cleanupHandler is an HTTP wrapper around cleanup() method that is designed to be triggered by App Engine Cron.
func cleanupHandler(w http.ResponseWriter, r *http.Request, config *tsbridge.Config) {
	ctx := r.Context()

	if env.IsAppEngine() && r.Header.Get("X-Appengine-Cron") != "true" {
		http.Error(w, "Only cron requests are allowed here", http.StatusUnauthorized)
		return
	}

	if err := cleanup(ctx, config); err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
}

// cleanup removes obsolete metric records. It is triggered by App Engine Cron.
func cleanup(ctx context.Context, config *tsbridge.Config) error {
	store, err := loadStorageEngine(ctx)
	if err != nil {
		return err
	}
	defer store.Close()

	metrics, err := tsbridge.NewMetricConfig(ctx, config, store)
	if err != nil {
		return err
	}

	var metricNames []string
	for _, m := range metrics.Metrics() {
		metricNames = append(metricNames, m.Name)
	}

	if err := store.CleanupRecords(ctx, metricNames); err != nil {
		return err
	}
	return nil
}

// index shows a web page with metric import status.
func index(w http.ResponseWriter, r *http.Request, config *tsbridge.Config) {
	if config.Options.EnableStatusPage != true {
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

	metrics, err := tsbridge.NewMetricConfig(ctx, config, storage)
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
	if err := t.Execute(w, metrics.Metrics()); err != nil {
		logAndReturnError(ctx, w, err)
	}
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
