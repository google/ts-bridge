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
	"fmt"
	"strings"
	"time"

	"github.com/influxdata/influxql"
)

// MetricConfig defines the configuration file parameters for a sepcific metric
// imported from InfluxDB.
type MetricConfig struct {
	Query          string `validate:"nonzero"`
	Database       string `validate:"nonzero"`
	Endpoint       string `validate:"nonzero"`
	Username       string
	Password       string
	TimeAggregated bool `yaml:"time_aggregated"`
	Cumulative     bool
}

func (c *MetricConfig) validateQuery() error {
	if c.TimeAggregated {
		if _, err := c.queryInterval(); err != nil {
			return err
		}
	}

	if c.Cumulative && !strings.Contains(strings.ToLower(c.Query), "cumulative_sum") {
		return fmt.Errorf("cumulative metric with query '%s' does not contain the cumulative_sum Influx function", c.Query)
	}

	return nil
}

func (c *MetricConfig) queryInterval() (time.Duration, error) {
	query, err := influxql.ParseQuery(c.Query)
	if err != nil {
		return 0, err
	}

	if len(query.Statements) != 1 {
		return 0, fmt.Errorf("expected 1 query statement, got %d", len(query.Statements))
	}

	selectStatement, ok := query.Statements[0].(*influxql.SelectStatement)
	if !ok {
		return 0, fmt.Errorf("failed to cast InfluxQL Statement to InfluxQL SelectStatement")
	}

	interval, err := selectStatement.GroupByInterval()
	if err != nil {
		return 0, err
	} else if interval <= 0 {
		return 0, fmt.Errorf("expected interval to be greater than 0, got %v", interval)
	}

	return interval, nil
}
