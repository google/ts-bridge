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

package record

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/appengine"
	"google.golang.org/appengine/aetest"
	"google.golang.org/appengine/datastore"
)

var testCtx context.Context

func TestMain(m *testing.M) {
	// Use strongly consistent datastore in tests to verify metric record cleanup.
	inst, err := aetest.NewInstance(&aetest.Options{StronglyConsistentDatastore: true})
	if err != nil {
		panic(err)
	}
	req, err := inst.NewRequest("GET", "/", nil)
	if err != nil {
		panic(err)
	}
	testCtx = appengine.NewContext(req)

	code := m.Run()
	inst.Close()
	os.Exit(code)
}

var metricRecordTests = []struct {
	name                 string
	success              bool
	points               int
	wantLastUpdateChange bool
}{
	{"error occurred", false, 0, false},
	{"0 points written", true, 0, false},
	{"5 points written", true, 5, true},
}

func TestDatastoreMetricRecords(t *testing.T) {
	for _, tt := range metricRecordTests {
		t.Run(tt.name, func(t *testing.T) {
			var err error

			// initialize the record with update time 1hr in the past.
			r := DatastoreMetricRecord{
				Name:        "metricname",
				Query:       "query",
				LastStatus:  "OK: all good",
				LastAttempt: time.Now().Add(-time.Hour),
				LastUpdate:  time.Now().Add(-time.Hour),
			}
			if err := r.write(testCtx); err != nil {
				t.Fatalf("error while initializing DatastoreMetricRecord: %v", err)
			}

			if tt.success {
				err = r.UpdateSuccess(testCtx, tt.points, "Test Message")
			} else {
				err = r.UpdateError(testCtx, fmt.Errorf("Test Message"))
			}
			if err != nil {
				t.Fatalf("error while updating DatastoreMetricRecord: %v", err)
			}

			rr := DatastoreMetricRecord{}
			if err := datastore.Get(testCtx, r.key(testCtx), &rr); err != nil {
				t.Fatalf("error while fetching DatastoreMetricRecord: %v", err)
			}

			if !strings.Contains(rr.LastStatus, "Test Message") {
				t.Errorf("expected to see LastStatus updated; got %v", rr.LastStatus)
			}

			if time.Now().Sub(rr.LastAttempt) > time.Minute {
				t.Errorf("expected to see LastAttempt updated; got %v", rr.LastAttempt)
			}

			if time.Now().Sub(rr.LastUpdate) > 2*time.Hour {
				t.Errorf("did not expect LastUpdate to be this old; got %v", rr.LastUpdate)
			}

			if tt.wantLastUpdateChange {
				if time.Now().Sub(rr.LastUpdate) > time.Minute {
					t.Errorf("expected to see LastAttempt updated; got %v", rr.LastUpdate)
				}
			} else {
				if time.Now().Sub(rr.LastUpdate) < time.Hour {
					t.Errorf("did not expect to see LastUpdate updated; got %v", rr.LastUpdate)
				}
			}
		})
	}
}

func TestCleanupDatastoreMetricRecords(t *testing.T) {
	for _, name := range []string{"metric1", "metric2"} {
		r := DatastoreMetricRecord{
			Name:        name,
			Query:       "query",
			LastStatus:  "OK: all good",
			LastAttempt: time.Now().Add(-time.Hour),
			LastUpdate:  time.Now().Add(-time.Hour),
		}
		if err := r.write(testCtx); err != nil {
			t.Fatalf("error while initializing DatastoreMetricRecord: %v", err)
		}
	}

	valid := []string{"metric1", "metric3"}

	if err := CleanupRecords(testCtx, valid); err != nil {
		t.Errorf("unexpected error from CleanupRecords: %v", err)
	}

	q := datastore.NewQuery(kindName)
	var records []*DatastoreMetricRecord
	if _, err := q.GetAll(testCtx, &records); err != nil {
		t.Fatalf("error while reading metric records: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %v", len(records))
	}
	if records[0].Name != "metric1" {
		t.Errorf("expected metric record for metric1; got %v", records[0])
	}
}
