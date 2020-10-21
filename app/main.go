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
	"fmt"
	"github.com/google/ts-bridge/tasks"
	"github.com/google/ts-bridge/web"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/google/ts-bridge/env"
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
		"sync-period", "Interval between syncs when running in standalone mode",
	).Envar("SYNC_PERIOD").Default("60s").Duration()

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
		UpdateTimeout:            *updateTimeout,
		EnableStatusPage:         *enableStatusPage,
		StorageEngine:            *storageEngine,
		SyncPeriod:               *syncPeriod,
	})

	h := web.NewHandler(config)

	http.HandleFunc("/", h.Index)
	http.HandleFunc("/sync", h.Sync)
	http.HandleFunc("/cleanup", h.Cleanup)
	http.HandleFunc("/health", h.Health)

	// Run a cleanup on startup
	log.Debugf("Performing startup cleanup...")
	if err := tasks.Cleanup(context.Background(), config); err != nil {
		log.Fatalf("error running the Cleanup() routine: %v", err)
	}

	// Run a sync loop for standalone use
	// TODO(temikus): refactor this to run exactly every SyncPeriod and skip sync if one is already active
	if !env.IsAppEngine() {
		log.Debug("Running outside of appengine, starting up a sync loop...")
		ctx, cancel := context.WithCancel(context.Background())
		go syncLoop(ctx, cancel, config)
	}

	// Build a connection string, e.g. ":8080"
	conn := net.JoinHostPort("", strconv.Itoa(*port))
	log.Debugf("Connection string: %v", conn)
	if err := http.ListenAndServe(conn, nil); err != nil {
		log.Fatalf("unable to start serving: %v", err)
	}

}

func syncLoop(ctx context.Context, cancel context.CancelFunc, config *tsbridge.Config) {
	defer cancel()
	for {
		select {
		case <-time.After(config.Options.SyncPeriod):
			ctx, cancel := context.WithTimeout(ctx, config.Options.UpdateTimeout)
			log.WithContext(ctx).Debugf("Running sync...")
			if err := tasks.Sync(ctx, config); err != nil {
				log.WithContext(ctx).Errorf("error running sync() routine: %v", err)
			}
			cancel()
		case <-ctx.Done():
			return
		}
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
