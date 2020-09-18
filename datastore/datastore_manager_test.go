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
	"testing"
	"time"

	"cloud.google.com/go/datastore"
)

func TestCleanupDatastoreMetricRecords(t *testing.T) {
	ctx := context.Background()
	storageManager := New(ctx, &Options{})

	for _, name := range []string{"metric1", "metric2"} {
		r := StoredMetricRecord{
			Name:        name,
			Query:       "query",
			LastStatus:  "OK: all good",
			LastAttempt: time.Now().Add(-time.Hour),
			LastUpdate:  time.Now().Add(-time.Hour),
			Storage:     storageManager,
		}
		if err := r.write(ctx); err != nil {
			t.Fatalf("error while initializing StoredMetricRecord: %v", err)
		}
	}

	valid := []string{"metric1", "metric3"}

	if err := storageManager.CleanupRecords(ctx, valid); err != nil {
		t.Errorf("unexpected error from CleanupRecords: %v", err)
	}

	q := datastore.NewQuery(kindName)
	var records []*StoredMetricRecord
	if _, err := storageManager.Client.GetAll(ctx, q, &records); err != nil {
		t.Fatalf("error while reading metric records: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %v", len(records))
	}
	if records[0].Name != "metric1" {
		t.Errorf("expected metric record for metric1; got %v", records[0])
	}
}
