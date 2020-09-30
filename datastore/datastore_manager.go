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
	"github.com/google/ts-bridge/env"

	"cloud.google.com/go/datastore"
	"github.com/google/ts-bridge/storage"
	log "github.com/sirupsen/logrus"
)

// Options holds settings specific to datastore
type Options struct {
	// Project sets the GCP project to use for communicating to datastore.
	Project string
}

// New initializes the Manager struct implementing a generic storage.Manager interface
func New(ctx context.Context, options *Options) *Manager {
	if options.Project == "" {
		if env.IsAppEngine() {
			options.Project = env.AppEngineProject()
			log.Infof("No datastore project specified, defaulting to GAE project: %v", options.Project)
		} else {
			log.Fatalf("Could not determine project to use for Datastore, please set DATASTORE_PROJECT or --datastore-project flag")
		}
	}

	dsClient, err := datastore.NewClient(ctx, options.Project)
	if err != nil {
		log.Fatalf("could not create datastore client: %v", err)
	}
	return &Manager{Client: dsClient}
}

// Manager struct implementing the storage.Manager interface
type Manager struct {
	Client *datastore.Client
}

// NewMetricRecord returns a Datastore-based metric record for a given metric name.
func (d *Manager) NewMetricRecord(ctx context.Context, name, query string) (storage.MetricRecord, error) {
	r := &StoredMetricRecord{Name: name, Storage: d}
	if err := r.load(ctx); err != nil {
		return nil, err
	}
	r.Query = query
	return r, nil
}

// CleanupRecords removes obsolete metric records from Datastore.
//   `keep` represents metrics to be kept, all others will be purged
func (d *Manager) CleanupRecords(ctx context.Context, keep []string) error {
	existing := make(map[string]bool)
	for _, m := range keep {
		existing[m] = true
	}
	q := datastore.NewQuery(kindName)
	var records []*StoredMetricRecord
	if _, err := d.Client.GetAll(ctx, q, &records); err != nil {
		return fmt.Errorf("could not list metric records: %v", err)
	}
	log.WithContext(ctx).Infof("%d metrics configured, %d metric records found in Datastore", len(keep), len(records))
	for _, r := range records {
		if !existing[r.Name] {
			log.WithContext(ctx).Infof("deleting obsolete metric record for %s", r.Name)
			err := d.Client.Delete(ctx, r.key(ctx))
			if err != nil {
				return fmt.Errorf("could not delete metric record %v: %v", r.Name, err)
			}
		}
	}
	return nil
}

// Close function exists here for compatibility as Datastore doesn't need to be closed
func (d *Manager) Close() error {
	return nil
}
