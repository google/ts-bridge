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
	"io/ioutil"
	"os"
	"testing"

	"github.com/timshannon/bolthold"
)

func TestBoltdbManager(t *testing.T) {
	// Create a temporary file for BoltDB
	tempFile, err := ioutil.TempFile("", "boltdb")
	if err != nil {
		t.Fatalf("Unable to create a temporary file for BoltDB: %v", err)
	}
	defer os.Remove(tempFile.Name())

	manager := New(&Options{DBPath: tempFile.Name()})
	defer manager.Close()

	testMetrics := []string{"metric1", "metric2"}

	for _, name := range testMetrics {
		// Create a new metric record
		record, err := manager.NewMetricRecord(nil, name, "test-query")
		if err != nil {
			t.Fatalf("Error creating a new metric record: %v", err)
		}

		// Call UpdateSuccess to write metric to Bolt
		record.UpdateSuccess(nil, 0, "0 points written")
	}

	// Create a var to store the result and check if the record exists in the store
	var records []StoredMetricRecord
	manager.Store.Find(&records, bolthold.Where("Query").Eq("test-query"))

	if records[0].Name != testMetrics[0] {
		t.Errorf("Expected to metric name to be metric1, but received: %v", records[0].Name)
	}

	if records[1].Name != testMetrics[1] {
		t.Errorf("Expected to metric name to be metric2, but received: %v", records[1].Name)
	}

	if len(records) != 2 {
		t.Errorf("expected 2 records, got %v", len(records))
	}

	// Try to cleanup records
	keep := []string{"metric1", "metric3"}
	if err := manager.CleanupRecords(nil, keep); err != nil {
		t.Fatalf("Error cleaning up records: %v", err)
	}

	// Reload records and verify that non-cleaned records still exist
	records = nil
	manager.Store.Find(&records, bolthold.Where("Query").Eq("test-query"))

	if len(records) > 1 {
		t.Errorf("expected 1 record to remain, got %v:%v", len(records), records)
	}

	if records[0].Name != "metric1" {
		t.Errorf("expected metric1 to be kept, got %v", records)
	}
}
