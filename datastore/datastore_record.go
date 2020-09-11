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
	"cloud.google.com/go/datastore"
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"time"
)

// Name of the Datastore kind where metric records are stored.
const kindName = "MetricRecords"

// StoredMetricRecord defines a Datastore entity that is used to store status information about an imported metric.
type StoredMetricRecord struct {
	Name        string
	Query       string
	LastUpdate  time.Time // last time we wrote any points to SD.
	LastAttempt time.Time // last time we attempted an update.
	LastStatus  string

	// CounterStartTime is used to keep start timestamp for cumulative metrics.
	CounterStartTime time.Time

	// Storage provides access to
	Storage *Manager
}

// Write metric data back to Datastore.
func (m *StoredMetricRecord) write(ctx context.Context) error {
	_, err := m.Storage.Client.Put(ctx, m.key(ctx), m)
	return err
}

// Load metric record state from Datastore.
func (m *StoredMetricRecord) load(ctx context.Context) error {
	err := m.Storage.Client.Get(ctx, m.key(ctx), m)
	if err != nil && err != datastore.ErrNoSuchEntity {
		return err
	}
	return nil
}

// key returns the Datastore key for a given metric record.
func (m *StoredMetricRecord) key(ctx context.Context) *datastore.Key {
	return datastore.NameKey(kindName, m.Name, nil)
}

// GetLastUpdate returns LastUpdate timestamp.
func (m *StoredMetricRecord) GetLastUpdate() time.Time {
	return m.LastUpdate
}

// GetCounterStartTime returns CounterStartTime.
func (m *StoredMetricRecord) GetCounterStartTime() time.Time {
	return m.CounterStartTime
}

// SetCounterStartTime sets CounterStartTime and persists metric data.
func (m *StoredMetricRecord) SetCounterStartTime(ctx context.Context, start time.Time) error {
	m.CounterStartTime = start
	return m.write(ctx)
}

// UpdateError updates metric status in Datastore with a given error message.
func (m *StoredMetricRecord) UpdateError(ctx context.Context, e error) error {
	log.WithContext(ctx).Errorf("%s: %s", m.Name, e)
	m.LastStatus = fmt.Sprintf("ERROR: %s", e)
	m.LastAttempt = time.Now()
	return m.write(ctx)
}

// UpdateSuccess updates metric status in Datastore with a given message.
func (m *StoredMetricRecord) UpdateSuccess(ctx context.Context, points int, msg string) error {
	log.WithContext(ctx).Infof("%s: %s", m.Name, msg)
	m.LastStatus = fmt.Sprintf("OK: %s", msg)
	m.LastAttempt = time.Now()
	if points > 0 {
		m.LastUpdate = time.Now()
	}
	return m.write(ctx)
}
