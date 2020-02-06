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
	"time"

	"github.com/google/ts-bridge/datadog"
	"github.com/google/ts-bridge/influxdb"

	"google.golang.org/appengine"
	// "google.golang.org/appengine/log"
	validator "gopkg.in/validator.v2"
	yaml "gopkg.in/yaml.v2"
)

// App Engine function to get current app id, which needs to be overridden in tests.
var appIDFunc = appengine.AppID

// Config is what the YAML configuration file gets deserialized to.
type Config struct {
	DatadogMetrics          []*DatadogMetricConfig `yaml:"datadog_metrics"`
	StackdriverDestinations []*DestinationConfig   `yaml:"stackdriver_destinations"`

	// internal list of metrics that gets populated when configuration file is read.
	metrics []*Metric
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

// Metrics returns a list of metrics defined in the configuration file.
func (c *Config) Metrics() []*Metric {
	return c.metrics
}

// ConfigOptions is a set of global options required to initialize configuration.
type ConfigOptions struct {
	Filename                    string
	DatadogMinPointAge          time.Duration
	DatadogCounterResetInterval time.Duration
}

// NewConfig reads and validates a configuration file, returning the Config struct.
func NewConfig(ctx context.Context, opts *ConfigOptions) (*Config, error) {
	data, err := ioutil.ReadFile(opts.Filename)
	if err != nil {
		return nil, err
	}
	c := &Config{}
	if err := yaml.UnmarshalStrict(data, c); err != nil {
		return nil, err
	}

	if err := validator.Validate(c); err != nil {
		return nil, fmt.Errorf("configuration file validation error: %s", err)
	}

	destinations := make(map[string]string)
	for _, d := range c.StackdriverDestinations {
		if _, ok := destinations[d.Name]; ok {
			return nil, fmt.Errorf("configuration file contains several destinations named '%s'", d.Name)
		}
		if d.ProjectID == "" {
			d.ProjectID = projectID(ctx)
		}
		if d.ProjectID == "" {
			return nil, fmt.Errorf("please provide project_id for destination '%s'", d.Name)
		}
		destinations[d.Name] = d.ProjectID
	}

	// metrics := make(map[string]bool)
	// for _, m := range c.DatadogMetrics {
	// 	project, ok := destinations[m.Destination]
	// 	if !ok {
	// 		return nil, fmt.Errorf("destination '%s' not found", m.Destination)
	// 	}
	// 	source, err := datadog.NewSourceMetric(m.Name, &m.MetricConfig, opts.DatadogMinPointAge, opts.DatadogCounterResetInterval)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("cannot create source metric '%s': %v", m.Name, err)
	// 	}
	// 	metric, err := NewMetric(ctx, m.Name, source, project)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("cannot create metric '%s': %v", m.Name, err)
	// 	}
	// 	c.metrics = append(c.metrics, metric)
	// 	if metrics[m.Name] {
	// 		return nil, fmt.Errorf("duplicate metric name '%s'", m.Name)
	// 	}
	// 	metrics[m.Name] = true
	// }

	m, err := influxdb.NewSourceMetric("foo", &influxdb.MetricConfig{
		Query:    "SELECT response_time FROM nginx_access_log",
		Database: "telegraf",
		Endpoint: "http://localhost:8086",
	}, time.Second, time.Hour)
	if err != nil {
		return nil, err
	}

	metric, err := NewMetric(ctx, m.Name, m, destinations["stackdriver"])
	if err != nil {
		return nil, err
	}

	c.metrics = append(c.metrics, metric)
	// log.Debugf(ctx, "Read %d metrics and %d destinations from the config file", len(metrics), len(destinations))
	return c, nil
}

// projectID returns the name of the App Engine app that code is running in.
// Empty string is returned if code is running in dev_appserver.py
func projectID(ctx context.Context) string {
	id := appIDFunc(ctx)
	// dev_appserver.py returns "None" as a string. ¯\_(ツ)_/¯
	if id == "None" {
		return ""
	}
	return id
}
