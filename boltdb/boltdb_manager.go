// Copyright 2020 Google LLC
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

package boltdb

import (
	"context"
	"fmt"
	"github.com/google/ts-bridge/storage"
	"os"
	"path"

	log "github.com/sirupsen/logrus"
	"github.com/timshannon/bolthold"
)

// Options holds storage settings specific to BoltDB.
type Options struct {
	DBPath string
}

// Manager struct implementing the storage.Manager interface
type Manager struct {
	Store *bolthold.Store
}

// New initializes the Manager struct implementing a generic storage.Manager interface
func New(options *Options) *Manager {
	// If path for a DB is not configured, default to working directory.
	boltPath := options.DBPath
	if boltPath == "" {
		pwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("couldn't get app working directory %v", err)
		}
		boltPath = path.Join(pwd, "bolt.db")
	}
	store, err := bolthold.Open(boltPath, 0664, nil)
	if err != nil {
		log.Fatalf("Unable to Open BoltDB at %v:%v", boltPath, err)
	}
	log.Debug("Opened BoltDB")

	return &Manager{Store: store}
}

// NewMetricRecord returns a BoltDB-based metric record for a given metric name
func (d *Manager) NewMetricRecord(_ context.Context, name, query string) (storage.MetricRecord, error) {
	r := &StoredMetricRecord{Name: name, storage: d}
	if err := r.load(); err != nil {
		return nil, err
	}
	r.Query = query
	return r, nil
}

// CleanupRecords removes obsolete metric records from BoltDB.
//   `keep` represents metrics to be kept, all others will be purged
func (d *Manager) CleanupRecords(ctx context.Context, keep []string) error {
	// datatype is just an example of the type stored so that the proper bucket and indexes are updated
	var datatype StoredMetricRecord
	query := bolthold.Where(bolthold.Key).Not().In(bolthold.Slice(keep)...)

	count, err := d.Store.Count(datatype, query)
	if err != nil {
		return fmt.Errorf("unable to count up records to delete: %v", err)
	}
	log.Infof("%d metrics configured, %d stale metric records found in BoltDB", len(keep), count)
	// Delete matching records
	if err := d.Store.DeleteMatching(datatype, query); err != nil {
		return fmt.Errorf("unable to clean up records from BoltDB: %v", err)
	}
	return nil
}

// Close properly closes the BoltDB file and removes the lock
func (d *Manager) Close() error {
	if err := d.Store.Close(); err != nil {
		// explicitly returning an error here since BoltDB file/lock errors can be cryptic without context
		return fmt.Errorf("could not close BoltDB store: %v", err)
	}
	return nil
}
