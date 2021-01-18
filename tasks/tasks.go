// Package tasks sets up basic ts-bridge routines used when running the application, e.g. sync(), cleanup(), etc.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/ts-bridge/boltdb"
	"github.com/google/ts-bridge/datastore"
	"github.com/google/ts-bridge/env"
	"github.com/google/ts-bridge/stackdriver"
	"github.com/google/ts-bridge/storage"
	"github.com/google/ts-bridge/tsbridge"
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

// Sync updates all configured metrics.
func Sync(ctx context.Context, config *tsbridge.Config, updateMetrics *tsbridge.UpdateMetrics) error {
	store, err := LoadStorageEngine(ctx, config)
	if err != nil {
		return err
	}
	defer store.Close()

	metrics, err := tsbridge.NewMetricConfig(ctx, config, store)
	if err != nil {
		return err
	}

	if errs := updateMetrics.All(ctx, metrics, config.Options.UpdateParallelism); errs != nil {
		msg := strings.Join(errs, "; ")
		return errors.New(msg)
	}
	return nil
}

// Cleanup removes obsolete metric records.
func Cleanup(ctx context.Context, config *tsbridge.Config) error {
	store, err := LoadStorageEngine(ctx, config)
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
