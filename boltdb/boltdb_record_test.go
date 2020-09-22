package boltdb

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"
)

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

func TestBoltDBMetricRecords(t *testing.T) {
	ctx := context.Background()

	// Create a temporary file for BoltDB
	tempFile, err := ioutil.TempFile("", "boltdb")
	if err != nil {
		t.Fatalf("Unable to create a temporary file for BoltDB: %v", err)
	}
	defer os.Remove(tempFile.Name())

	manager := New(&Options{DBPath: tempFile.Name()})
	defer manager.Close()

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
				storage:     manager,
			}
			if err := r.write(); err != nil {
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

			rr := StoredMetricRecord{
				Name:    "metricname",
				storage: manager,
			}
			if err := rr.load(); err != nil {
				t.Fatalf("error while loading StoredMetricRecord: %v", err)
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
