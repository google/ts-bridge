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

package tsbridge

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/ts-bridge/mocks"

	"github.com/golang/mock/gomock"
	"google.golang.org/appengine/datastore"
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

func TestMetricRecords(t *testing.T) {
	for _, tt := range metricRecordTests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()
			mock := mocks.NewMockSourceMetric(mockCtrl)
			mock.EXPECT().Query().Return("new-query")

			// initialize the record with update time 1hr in the past.
			r := MetricRecord{
				Name:        "metricname",
				Query:       "old-query",
				LastStatus:  "OK: all good",
				LastAttempt: time.Now().Add(-time.Hour),
				LastUpdate:  time.Now().Add(-time.Hour),
			}
			if err := r.write(testCtx); err != nil {
				t.Fatalf("error while initializing MetricRecord: %v", err)
			}

			m, err := NewMetric(testCtx, "metricname", mock, "sd-project")
			if err != nil {
				t.Fatalf("error while creating metric: %v", err)
			}
			if tt.success {
				err = m.RecordSuccess(testCtx, tt.points, "Test Message")
			} else {
				err = m.RecordError(testCtx, fmt.Errorf("Test Message"))
			}
			if err != nil {
				t.Fatalf("error while updating MetricRecord: %v", err)
			}

			r = MetricRecord{}
			if err := datastore.Get(testCtx, m.Record.key(testCtx), &r); err != nil {
				t.Fatalf("error while fetching MetricRecord: %v", err)
			}

			if !strings.Contains(r.LastStatus, "Test Message") {
				t.Errorf("expected to see LastStatus updated; got %v", r.LastStatus)
			}

			if !strings.Contains(r.Query, "new-query") {
				t.Errorf("expected to see Query updated; got %v", r.Query)
			}

			if time.Now().Sub(r.LastAttempt) > time.Minute {
				t.Errorf("expected to see LastAttempt updated; got %v", r.LastAttempt)
			}

			if time.Now().Sub(r.LastUpdate) > 2*time.Hour {
				t.Errorf("did not expect LastUpdate to be this old; got %v", r.LastUpdate)
			}

			if tt.wantLastUpdateChange {
				if time.Now().Sub(r.LastUpdate) > time.Minute {
					t.Errorf("expected to see LastAttempt updated; got %v", r.LastUpdate)
				}
			} else {
				if time.Now().Sub(r.LastUpdate) < time.Hour {
					t.Errorf("did not expect to see LastUpdate updated; got %v", r.LastUpdate)
				}
			}
		})
	}
}
