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
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/timshannon/bolthold"
)

// StoredMetricRecord defines a BoltDB entity that is used to store status information about an imported metric.
type StoredMetricRecord struct {
	Name        string
	Query       string
	LastUpdate  time.Time // last time we wrote any points to SD.
	LastAttempt time.Time // last time we attempted an update.
	LastStatus  string

	// CounterStartTime is used to keep start timestamp for cumulative metrics.
	CounterStartTime time.Time

	storage *Manager
}

// Write metric data back to BoltDB.
func (m *StoredMetricRecord) write() error {
	return m.storage.Store.Upsert(m.Name, m)
}

// Load metric record state from BoltDB.
func (m *StoredMetricRecord) load() error {
	if err := m.storage.Store.Get(m.Name, m); err != nil && err != bolthold.ErrNotFound {
		return err
	}
	return nil
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
func (m *StoredMetricRecord) SetCounterStartTime(_ context.Context, start time.Time) error {
	m.CounterStartTime = start
	return m.write()
}

// UpdateError updates metric status in BoltDB with a given error message.
func (m *StoredMetricRecord) UpdateError(_ context.Context, e error) error {
	log.Errorf("%s: %s", m.Name, e)
	m.LastStatus = fmt.Sprintf("ERROR: %s", e)
	m.LastAttempt = time.Now()
	return m.write()
}

// UpdateSuccess updates metric status in BoltDB with a given message.
func (m *StoredMetricRecord) UpdateSuccess(_ context.Context, points int, msg string) error {
	log.Infof("%s: %s", m.Name, msg)
	m.LastStatus = fmt.Sprintf("OK: %s", msg)
	m.LastAttempt = time.Now()
	if points > 0 {
		m.LastUpdate = time.Now()
	}
	return m.write()
}
