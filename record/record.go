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

// Package record describes Metric Records that store some data about each imported metric in App Engine Datastore.
package record

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
)

//go:generate mockgen -destination=../mocks/mock_metric_record.go -package=mocks github.com/google/ts-bridge/record MetricRecord

// MetricRecord is an interface implemented by DatastoreMetricRecord.
type MetricRecord interface {
	UpdateError(ctx context.Context, e error) error
	UpdateSuccess(ctx context.Context, points int, msg string) error
	GetLastUpdate() time.Time
	GetCounterStartTime() time.Time
	SetCounterStartTime(ctx context.Context, start time.Time) error
}

// Name of the Datastore kind where metric records are stored.
const kindName = "MetricRecords"

// DatastoreMetricRecord defines a Datastore entity that is used to store status information about an imported metric.
type DatastoreMetricRecord struct {
	Name        string
	Query       string
	LastUpdate  time.Time // last time we wrote any points to SD.
	LastAttempt time.Time // last time we attempted an update.
	LastStatus  string

	// CounterStartTime is used to keep start timestamp for cumulative metrics.
	CounterStartTime time.Time
}

// NewDatastoreMetricRecord returns a Datastore-based metric record for a given metric name.
func NewDatastoreMetricRecord(ctx context.Context, name, query string) (*DatastoreMetricRecord, error) {
	r := &DatastoreMetricRecord{Name: name}
	if err := r.load(ctx); err != nil {
		return nil, err
	}
	r.Query = query
	return r, nil
}

// Write metric data back to Datastore.
func (m *DatastoreMetricRecord) write(ctx context.Context) error {
	_, err := datastore.Put(ctx, m.key(ctx), m)
	return err
}

// Load metric record state from Datastore.
func (m *DatastoreMetricRecord) load(ctx context.Context) error {
	err := datastore.Get(ctx, m.key(ctx), m)
	if err != nil && err != datastore.ErrNoSuchEntity {
		return err
	}
	return nil
}

// key returns the Datastore key for a given metric record.
func (m *DatastoreMetricRecord) key(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, kindName, m.Name, 0, nil)
}

// GetLastUpdate returns LastUpdate timestamp.
func (m *DatastoreMetricRecord) GetLastUpdate() time.Time {
	return m.LastUpdate
}

// GetCounterStartTime returns CounterStartTime.
func (m *DatastoreMetricRecord) GetCounterStartTime() time.Time {
	return m.CounterStartTime
}

// SetCounterStartTime sets CounterStartTime and persists metric data.
func (m *DatastoreMetricRecord) SetCounterStartTime(ctx context.Context, start time.Time) error {
	m.CounterStartTime = start
	return m.write(ctx)
}

// UpdateError updates metric status in Datastore with a given error message.
func (m *DatastoreMetricRecord) UpdateError(ctx context.Context, e error) error {
	log.Errorf(ctx, "%s: %s", m.Name, e)
	m.LastStatus = fmt.Sprintf("ERROR: %s", e)
	m.LastAttempt = time.Now()
	return m.write(ctx)
}

// UpdateSuccess updates metric status in Datastore with a given message.
func (m *DatastoreMetricRecord) UpdateSuccess(ctx context.Context, points int, msg string) error {
	log.Infof(ctx, "%s: %s", m.Name, msg)
	m.LastStatus = fmt.Sprintf("OK: %s", msg)
	m.LastAttempt = time.Now()
	if points > 0 {
		m.LastUpdate = time.Now()
	}
	return m.write(ctx)
}

// CleanupRecords removes obsolete metric records from Datastore.
func CleanupRecords(ctx context.Context, valid []string) error {
	existing := make(map[string]bool)
	for _, m := range valid {
		existing[m] = true
	}
	q := datastore.NewQuery(kindName)
	var records []*DatastoreMetricRecord
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
