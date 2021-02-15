// Package tasks sets up basic ts-bridge routines used when running the application, e.g. sync(), cleanup(), etc.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/ts-bridge/boltdb"
	"github.com/google/ts-bridge/datastore"
	"github.com/google/ts-bridge/env"
	"github.com/google/ts-bridge/stackdriver"
	"github.com/google/ts-bridge/storage"
	"github.com/google/ts-bridge/tsbridge"
	log "github.com/sirupsen/logrus"
)

var (
	sdClient       *stackdriver.Adapter
	statsCollector *tsbridge.StatsCollector
)

// LoadStorageEngine is a helper function to load the correct storage manager depending on settings
func LoadStorageEngine(ctx context.Context, config *tsbridge.Config) (storage.Manager, error) {
	switch config.Options.StorageEngine {
	case "datastore":
		datastoreManager := datastore.New(ctx, &datastore.Options{Project: config.Options.DatastoreProject})
		return datastoreManager, nil
	case "boltdb":
		if env.IsAppEngine() {
			return nil, errors.New("BoltDB storage is not supported on AppEngine")
		}
		opts := &boltdb.Options{DBPath: config.Options.BoltdbPath}

		return boltdb.New(opts), nil
	default:
		return nil, fmt.Errorf("unknown storage engine selected: %s", config.Options.StorageEngine)
	}
}

// isMetricCfgUpdated checks if the metric config file has been updated.
func isMetricCfgUpdated(ctx context.Context, filename string, metricCfgFs *os.FileInfo) (bool, error) {
	fs, err := os.Stat(filename)
	if err != nil {
		return false, err
	}
	return (*metricCfgFs).ModTime() != fs.ModTime(), nil
}

// SyncMetricConfig ensures that metric config is always synced with the metric config file. This should only be called when !env.IsAppEngine().
func SyncMetricConfig(ctx context.Context, config *tsbridge.Config, store storage.Manager, metricCfg *tsbridge.MetricConfig) error {
	update, err := isMetricCfgUpdated(ctx, config.Options.Filename, metricCfg.FileInfo)
	if err != nil {
		return err
	}
	if update {
		updatedMetricCfg, err := tsbridge.NewMetricConfig(ctx, config, store)
		log.Debug("Metric Config file changes reloaded.")
		if err != nil {
			return err
		}
		*metricCfg = *updatedMetricCfg
	}
	return nil
}

// SyncMetrics updates all configured metrics.
func SyncMetrics(ctx context.Context, config *tsbridge.Config, metrics *tsbridge.Metrics, metricCfg *tsbridge.MetricConfig) error {
	if errs := metrics.UpdateAll(ctx, metricCfg, config.Options.UpdateParallelism); errs != nil {
		msg := strings.Join(errs, "; ")
		return errors.New(msg)
	}
	return nil
}

// Cleanup removes obsolete metric records.
func Cleanup(ctx context.Context, config *tsbridge.Config, store storage.Manager) error {
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
