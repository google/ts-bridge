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

package influxdb

import (
	"testing"
	"time"
)

func TestParsingQueryInterval(t *testing.T) {
	for _, tt := range []struct {
		description  string
		query        string
		wantErr      bool
		wantInterval time.Duration
	}{
		{
			description: "error when no time interval",
			query:       "SELECT time FROM foo",
			wantErr:     true,
		},
		{
			description: "error when no unit",
			query:       "SELECT MEAN(bar) FROM foo GROUP BY time(100)",
			wantErr:     true,
		},
		{
			description:  "correctly converts seconds",
			query:        "SELECT MEAN(bar) FROM foo GROUP BY time(10s)",
			wantInterval: 10 * time.Second,
		},
		{
			description:  "correctly converts minutes",
			query:        "SELECT MEAN(bar) FROM foo GROUP BY time(7m)",
			wantInterval: 7 * time.Minute,
		},
		{
			description:  "correctly converts hours",
			query:        "SELECT MEAN(bar) FROM foo GROUP BY time(2h)",
			wantInterval: 2 * time.Hour,
		},
		{
			description:  "correctly converts days",
			query:        "SELECT MEAN(bar) FROM foo GROUP BY time(1d)",
			wantInterval: 24 * time.Hour,
		},
		{
			description:  "correctly converts weeks",
			query:        "SELECT MEAN(bar) FROM foo GROUP BY time(3w)",
			wantInterval: 24 * time.Hour * 7 * 3,
		},
	} {
		t.Run(tt.description, func(t *testing.T) {
			c := &MetricConfig{Query: tt.query}
			i, err := c.queryInterval()
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("unexpected error parsing interval for %s: %v", tt.query, err)
				}
				return
			}

			if tt.wantErr {
				t.Fatalf("expected error parsing interval for %s, got interval: %v", tt.query, i)
			}

			if i != tt.wantInterval {
				t.Errorf("expected interval %v, got %v", tt.wantInterval, i)
			}
		})
	}
}
