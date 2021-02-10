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

// Package tsbridge deals with Time Series Bridge configuration files and metric representations.
// This file has code related to configuration files.
package tsbridge

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/google/ts-bridge/datadog"
	"github.com/google/ts-bridge/env"
	"github.com/google/ts-bridge/influxdb"
	"github.com/google/ts-bridge/storage"
	log "github.com/sirupsen/logrus"
	validator "gopkg.in/validator.v2"
	yaml "gopkg.in/yaml.v2"
)

// MetricConfig is what the YAML configuration file gets deserialized to.
type MetricConfig struct {
	DatadogMetrics  []*DatadogMetricConfig  `yaml:"datadog_metrics"`
	InfluxDBMetrics []*InfluxDBMetricConfig `yaml:"influxdb_metrics"`

	StackdriverDestinations []*DestinationConfig `yaml:"stackdriver_destinations"`

	// internal list of metrics that gets populated when configuration file is read.
	metrics []*Metric

	FileInfo *os.FileInfo
}

// DestinationConfig defines configuration for a Stackdriver project metrics are written to.
// Name is only used internally to set destination for a specific imported metric.
type DestinationConfig struct {
	Name      string `validate:"nonzero"`
	ProjectID string `yaml:"project_id" validate:"regexp=^[A-Za-z0-9:.-]*$"`
}

// SourceMetricConfig defines some common parameters that any imported metric must have, irrespective of the
// monitoring system data is coming from.
type SourceMetricConfig struct {
	Name        string `validate:"regexp=^[A-Za-z0-9]\\w*$"`
	Destination string `validate:"nonzero"`
}

// DatadogMetricConfig combines common metric configuration parameters with Datadog-specific ones.
type DatadogMetricConfig struct {
	SourceMetricConfig   `yaml:"_,inline"`
	datadog.MetricConfig `yaml:"_,inline"`
}

// InfluxDBMetricConfig combines common metric configuration parameters with InfluxDB-specific ones.
type InfluxDBMetricConfig struct {
	SourceMetricConfig    `yaml:"_,inline"`
	influxdb.MetricConfig `yaml:"_,inline"`
}

// Metrics returns a list of metrics defined in the configuration file.
func (c *MetricConfig) Metrics() []*Metric {
	return c.metrics
}

// NewMetricConfig reads and validates a configuration file, returning the MetricConfig struct.
func NewMetricConfig(ctx context.Context, config *Config, storage storage.Manager) (*MetricConfig, error) {
	data, err := ioutil.ReadFile(config.Options.Filename)
	if err != nil {
		return nil, err
	}
	c := &MetricConfig{}
	if err := yaml.UnmarshalStrict(data, c); err != nil {
		return nil, err
	}

	if err := validator.Validate(c); err != nil {
		return nil, fmt.Errorf("configuration file validation error: %s", err)
	}

	fileInfo, err := os.Stat(config.Options.Filename)
	if err != nil {
		return nil, err
	}
	c.FileInfo = &fileInfo

	destinations := make(map[string]string)
	for _, d := range c.StackdriverDestinations {
		if _, ok := destinations[d.Name]; ok {
			return nil, fmt.Errorf("configuration file contains several destinations named '%s'", d.Name)
		}
		if d.ProjectID == "" {
			d.ProjectID = env.AppEngineProject()
		}
		if d.ProjectID == "" {
			return nil, fmt.Errorf("please provide project_id for destination '%s'", d.Name)
		}
		destinations[d.Name] = d.ProjectID
	}

	// Map used to ensure that metric names are unique.
	metrics := make(map[string]bool)
	// Function to create a new source metric, and to add it to the current configuration.
	addSourceMetric := func(name, dest string, sourceMetric SourceMetric) error {
		project, ok := destinations[dest]
		if !ok {
			return fmt.Errorf("destination '%s' not found", dest)
		}
		metric, err := NewMetric(ctx, name, sourceMetric, project, storage)
		if err != nil {
			return fmt.Errorf("cannot create metric '%s': %v", name, err)
		}

		c.metrics = append(c.metrics, metric)
		if metrics[name] {
			return fmt.Errorf("duplicate metric name '%s'", name)
		}
		metrics[name] = true
		return nil
	}

	for _, m := range c.DatadogMetrics {
		metric, err := datadog.NewSourceMetric(m.Name, &m.MetricConfig, config.Options.MinPointAge, config.Options.CounterResetInterval)
		if err != nil {
			return nil, fmt.Errorf("cannot create Datadog source metric '%s': %v", m.Name, err)
		}

		if err = addSourceMetric(m.Name, m.Destination, metric); err != nil {
			return nil, err
		}
	}

	for _, m := range c.InfluxDBMetrics {
		metric, err := influxdb.NewSourceMetric(m.Name, &m.MetricConfig, config.Options.MinPointAge, config.Options.CounterResetInterval)
		if err != nil {
			return nil, fmt.Errorf("cannot create InfluxDB source metric '%s': %v", m.Name, err)
		}

		if err = addSourceMetric(m.Name, m.Destination, metric); err != nil {
			return nil, err
		}
	}

	log.WithContext(ctx).Debugf("Read %d metrics and %d destinations from the config file", len(metrics), len(destinations))
	return c, nil
}
