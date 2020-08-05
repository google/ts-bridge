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

package datastore

import (
	"context"
	"fmt"
	"github.com/google/ts-bridge/storage"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
)

// New initializes the Manager struct implementing a generic storage.Manager interface
func New() *Manager {
	return &Manager{}
}

// Manager struct implementing the storage.Manager interface
type Manager struct {
}

// NewMetricRecord returns a Datastore-based metric record for a given metric name.
func (d *Manager) NewMetricRecord(ctx context.Context, name, query string) (storage.MetricRecord, error) {
	r := &StoredMetricRecord{Name: name}
	if err := r.load(ctx); err != nil {
		return nil, err
	}
	r.Query = query
	return r, nil
}

// CleanupRecords removes obsolete metric records from Datastore.
func (d *Manager) CleanupRecords(ctx context.Context, valid []string) error {
	existing := make(map[string]bool)
	for _, m := range valid {
		existing[m] = true
	}
	q := datastore.NewQuery(kindName)
	var records []*StoredMetricRecord
	if _, err := q.GetAll(ctx, &records); err != nil {
		return fmt.Errorf("could not list metric records: %v", err)
	}
	log.Infof(ctx, "%d metrics configured, %d metric records found in Datastore", len(valid), len(records))
	for _, r := range records {
		if !existing[r.Name] {
			log.Infof(ctx, "deleting obsolete metric record for %s", r.Name)
			err := datastore.Delete(ctx, r.key(ctx))
			if err != nil {
				return fmt.Errorf("could not delete metric record %v: %v", r.Name, err)
			}
		}
	}
	return nil
}
