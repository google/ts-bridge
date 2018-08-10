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

// Package tsbridge deals with Time Series Bridge configuration files and metric representations.
// This file describes Metric Records that store some data about each imported metric in App Engine Datastore.
package tsbridge

import (
	"context"
	"time"

	"google.golang.org/appengine/datastore"
)

// Name of the Datastore kind where metric records are stored.
const kindName = "MetricRecords"

// MetricRecord defines a Datastore entity that is used to store status information about an imported metric.
type MetricRecord struct {
	Name        string
	Query       string
	LastUpdate  time.Time // last time we wrote any points to SD.
	LastAttempt time.Time // last time we attempted an update.
	LastStatus  string
}

// Write metric data back to Datastore.
// TODO(knyar): add a way to clean up unused records.
func (m *MetricRecord) write(ctx context.Context) error {
	_, err := datastore.Put(ctx, m.key(ctx), m)
	return err
}

// Load metric record state from Datastore.
func (m *MetricRecord) load(ctx context.Context) error {
	err := datastore.Get(ctx, m.key(ctx), m)
	if err != nil && err != datastore.ErrNoSuchEntity {
		return err
	}
	return nil
}

// key returns the Datastore key for a given metric record.
func (m *MetricRecord) key(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, kindName, m.Name, 0, nil)
}

// ListRecords returns a list of metric records from Datastore.
func ListRecords(ctx context.Context) ([]*MetricRecord, error) {
	q := datastore.NewQuery(kindName)
	var r []*MetricRecord
	_, err := q.GetAll(ctx, &r)
	return r, err
}
