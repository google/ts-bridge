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
	"regexp"
	"strconv"
	"time"
)

// MetricConfig defines the configuration file parameters for a sepcific metric
// imported from InfluxDB.
type MetricConfig struct {
	Query          string
	Database       string
	Endpoint       string
	Username       string
	Password       string
	TimeAggregated bool
}

func (c *MetricConfig) validateQuery() error {
	if c.TimeAggregated {
		if _, err := c.queryInterval(); err != nil {
			return err
		}
	}

	return nil
}

func (c *MetricConfig) queryInterval() (time.Duration, error) {
	// Case-insensitive match of time(<numeric duration><unit>) in an InfluxQL
	// query, which captures the numberic duration and unit seperately.
	re := regexp.MustCompile(`(?i)time\(([0-9]+)([uµsmhdw]|ns|ms)\)`)
	matches := re.FindAllStringSubmatch(c.Query, -1)

	if len(matches) != 1 {
		return 0, fmt.Errorf("query %s has %d time groupings, expected 1", c.Query, len(matches))
	}
	if len(matches[0]) != 3 {
		return 0, fmt.Errorf("query %s expected {match} {magnitude} {unit} match, got %v", c.Query, matches[0])
	}

	return parseDuration(matches[0][1], matches[0][2])
}

func parseDuration(val, unit string) (time.Duration, error) {
	// Convert the InfluxQL time units into something Golang can understand.
	if unit == "u" || unit == "µ" {
		unit = "us"
	} else if unit == "d" || unit == "w" {
		v, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("failed to parse %s to int", val)
		}

		v = v * 24
		if unit == "w" {
			v = v * 7
		}

		val, unit = strconv.Itoa(v), "h"
	}

	return time.ParseDuration(val + unit)
}
