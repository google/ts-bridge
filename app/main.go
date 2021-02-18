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
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/google/ts-bridge/stackdriver"
	"github.com/google/ts-bridge/tasks"
	"github.com/google/ts-bridge/web"

	"github.com/google/ts-bridge/env"
	"github.com/google/ts-bridge/storage"
	"github.com/google/ts-bridge/tsbridge"
	"github.com/google/ts-bridge/version"

	"cloud.google.com/go/profiler"
	"github.com/pkg/profile"
	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	debug = kingpin.Flag("debug", "enable debug mode").Envar("DEBUG").Default("false").Bool()
	port  = kingpin.Flag("port", "ts-bridge server port").Envar("PORT").Default("8080").Int()

	metricConfig = kingpin.Flag(
		"metric-config", "metric configuration file path",
	).Envar("METRIC_CONFIG").Default("metrics.yaml").String()

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

	statsSDProject = kingpin.Flag(
		"stats-sd-project", "Stackdriver project for internal ts-bridge metrics",
	).Envar("STATS_SD_PROJECT").String()

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

	enableCloudProfiler = kingpin.Flag(
		"enable-cloud-profiler", "Enable GCP Cloud profiler",
	).Envar("ENABLE_CLOUD_PROFILER").Bool()

	boltdbPath = kingpin.Flag("boltdb-path", "path to BoltDB store, e.g. /data/bolt.db").Envar("BOLTDB_PATH").String()

	ver = kingpin.Flag("version", "print the current version revision").Bool()

	monitoringBackends = kingpin.Flag(
		"stats-metric-exporters", "Monitoring backend(s) for internal metrics.",
	).Envar("STATS_METRIC_EXPORTERS").Default("stackdriver").Enums("prometheus", "stackdriver")

	enableLocalCPUProfiler = kingpin.Flag(
		"enable-local-cpu-profiler", "Enable local CPU Profiler",
	).Envar("ENABLE_LOCAL_CPU_PROFILER").Default("false").Bool()
)

func main() {
	kingpin.Parse()

	if *enableCloudProfiler {
		startCloudProfiler()
	}

	if *ver {
		fmt.Printf("%s", version.Revision())
		return
	}

	processLegacyStringVar("CONFIG_FILE", metricConfig, "METRIC_CONFIG")
	processLegacyStringVar("SD_PROJECT_FOR_INTERNAL_METRICS", statsSDProject, "STATS_SD_PROJECT")

	if *debug {
		log.SetLevel(log.DebugLevel)
		log.Debug("Debug logging enabled...")
	}

	if err := validateFlags(); err != nil {
		log.Fatalf("Invalid flags: %v", err)
	}

	if *enableLocalCPUProfiler {
		log.Debugf("CPU Profiler enabled.")
		defer profile.Start(profile.CPUProfile, profile.ProfilePath(".")).Stop()
	}

	config := tsbridge.NewConfig(&tsbridge.ConfigOptions{
		Filename:                 *metricConfig,
		MinPointAge:              *minPointAge,
		CounterResetInterval:     *counterResetInterval,
		SDLookBackInterval:       *sdLookBackInterval,
		SDInternalMetricsProject: *statsSDProject,
		UpdateParallelism:        *updateParallelism,
		UpdateTimeout:            *updateTimeout,
		EnableStatusPage:         *enableStatusPage,
		MonitoringBackends:       *monitoringBackends,
		StorageEngine:            *storageEngine,
		SyncPeriod:               *syncPeriod,
	})

	ctx, cancel := context.WithCancel(context.Background())
	metrics, err := CreateMetrics(ctx, config)
	if err != nil {
		log.Fatalf("failed initializing adaptor/collector dependencies: %v", err)
	}
	defer cleanup(metrics)

	store, err := tasks.LoadStorageEngine(ctx, config)
	if err != nil {
		log.Fatalf("failed to load storage engine: %v", err)
	}
	defer store.Close()

	metricCfg, err := tsbridge.NewMetricConfig(ctx, config, store)
	if err != nil {
		log.Fatalf("failed to perform initial load of metric config: %v", err)
	}

	h := web.NewHandler(config, metrics, metricCfg, store)
	http.HandleFunc("/", h.Index)
	http.HandleFunc("/sync", h.Sync)
	http.HandleFunc("/cleanup", h.Cleanup)
	http.HandleFunc("/health", h.Health)

	// Run a cleanup on startup
	log.Debugf("Performing startup cleanup...")
	if err := tasks.Cleanup(ctx, config, store); err != nil {
		log.Fatalf("error running the Cleanup() routine: %v", err)
	}

	// Run a sync loop for standalone use
	// TODO(temikus): refactor this to run exactly every SyncPeriod and skip sync if one is already active
	if !env.IsAppEngine() {
		log.Debug("Running outside of appengine, starting up a sync loop...")
		go syncLoop(ctx, cancel, config, metrics, metricCfg, store)
	}

	// Build a connection string, e.g. ":8080"
	conn := net.JoinHostPort("", strconv.Itoa(*port))
	log.Debugf("Connection string: %v", conn)
	if err := http.ListenAndServe(conn, nil); err != nil {
		log.Fatalf("unable to start serving: %v", err)
	}

}

func syncLoop(ctx context.Context, cancel context.CancelFunc, config *tsbridge.Config, metrics *tsbridge.Metrics, metricCfg *tsbridge.MetricConfig, store storage.Manager) {
	defer cancel()

	for {
		select {
		case <-time.After(config.Options.SyncPeriod):
			log.Debugf("Goroutines: %v", runtime.NumGoroutine())
			ctx, cancel := context.WithTimeout(ctx, config.Options.UpdateTimeout)
			log.WithContext(ctx).Debugf("Running sync...")
			if err := tasks.SyncMetricConfig(ctx, config, store, metricCfg); err != nil {
				log.WithContext(ctx).Errorf("error running SyncMetricConfig() within sync() routine: %v", err)
			}
			if err := tasks.SyncMetrics(ctx, config, metrics, metricCfg); err != nil {
				log.WithContext(ctx).Errorf("error running SyncMetrics() routine: %v", err)
			}
			cancel()
		case <-ctx.Done():
			return
		}
	}
}

// CreateMetrics returns a Metrics struct containing external dependencies.
func CreateMetrics(ctx context.Context, config *tsbridge.Config) (*tsbridge.Metrics, error) {
	sd, err := stackdriver.NewAdapter(ctx, config.Options.SDLookBackInterval)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize stackdriver adapter: %v", err)
	}
	sc, err := tsbridge.NewCollector(ctx, config.Options.SDInternalMetricsProject, config.Options.MonitoringBackends)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize stats collector: %v", err)
	}
	return tsbridge.New(ctx, sd, sc), nil
}

func cleanup(metrics *tsbridge.Metrics) {
	defer metrics.StatsCollector.Close()
	if err := metrics.SDClient.Close(); err != nil {
		log.Fatalf("Could not close Stackdriver client: %v", err)
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

func processLegacyStringVar(legacyVar string, flag *string, replacement string) {
	v := os.Getenv(legacyVar)
	if v != "" {
		log.Warnf("Legacy env option %v is set, it will be deprecated in next major version in favour of %v",
			legacyVar, replacement)
		// Dereference flag into a new value
		*flag = v
	}
}

func startCloudProfiler() {
	// Note: this will work only while running in GCP, `project` parameter will need to be piped in if standalone
	// profiling is needed
	cfg := profiler.Config{
		Service:        "ts-bridge",
		ServiceVersion: version.Revision(),
	}

	// Profiler initialization, best done as early as possible.
	if err := profiler.Start(cfg); err != nil {
		log.Warningf("unable to start cloud profiler: %v", err)
	}
}
