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

package datastore

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())
	// Save the emulator's quit channel.
	quit := Emulator(ctx, true)
	code := m.Run()
	cancel()
	// Wait for channel close before exiting the test suite
	<-quit
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
	ctx := context.Background()
	storageManager := New(ctx, &Options{})

	for _, tt := range metricRecordTests {
		t.Run(tt.name, func(t *testing.T) {
			var err error

			// initialize the record with update time 1hr in the past.
			r := StoredMetricRecord{
				Name:        "metricname",
				Query:       "query",
				LastStatus:  "OK: all good",
				LastAttempt: time.Now().Add(-time.Hour),
				LastUpdate:  time.Now().Add(-time.Hour),
				Storage:     storageManager,
			}
			if err := r.write(ctx); err != nil {
				t.Fatalf("error while initializing StoredMetricRecord: %v", err)
			}

			if tt.success {
				err = r.UpdateSuccess(ctx, tt.points, "Test Message")
			} else {
				err = r.UpdateError(ctx, fmt.Errorf("Test Message"))
			}

			if err != nil {
				t.Fatalf("error while updating StoredMetricRecord: %v", err)
			}

			rr := StoredMetricRecord{Storage: storageManager}
			if err := rr.Storage.Client.Get(ctx, r.key(ctx), &rr); err != nil {
				t.Fatalf("error while fetching StoredMetricRecord: %v", err)
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
