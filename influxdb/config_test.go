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

func TestQueryValidation(t *testing.T) {
	for _, tt := range []struct {
		description string
		config      *MetricConfig
		wantErr     bool
	}{
		{
			description: "ok for valid gauge metrics",
			config: &MetricConfig{
				Query: "SELECT * FROM foo",
			},
		},
		{
			description: "ok for valid time aggregated gauge metrics",
			config: &MetricConfig{
				Query:          "SELECT * FROM foo GROUP BY time(1s)",
				TimeAggregated: true,
			},
		},
		{
			description: "ok for valid cumulative metrics",
			config: &MetricConfig{
				Query:      "SELECT CUMULATIVE_SUM(bar) FROM foo",
				Cumulative: true,
			},
		},
		{
			description: "ok for valid cumulative, time aggregated metrics",
			config: &MetricConfig{
				Query:          "SELECT CUMULATIVE_SUM(bar) FROM foo GROUP BY time(1s)",
				TimeAggregated: true,
				Cumulative:     true,
			},
		},
		{
			description: "error when time aggregated queries don't have group by interval",
			config: &MetricConfig{
				Query:          "SELECT * FROM foo",
				TimeAggregated: true,
			},
			wantErr: true,
		},
		{
			description: "error when cumulative queries don't have cumulative sum function",
			config: &MetricConfig{
				Query:          "SELECT * FROM foo GROUP BY time(1s)",
				TimeAggregated: true,
				Cumulative:     true,
			},
			wantErr: true,
		},
	} {
		t.Run(tt.description, func(t *testing.T) {
			err := tt.config.validateQuery()
			if err != nil && !tt.wantErr {
				t.Errorf("expected no errors, got %v", err)
			}
			if err == nil && tt.wantErr {
				t.Error("expected error, got nil")
			}
		})
	}
}

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
