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
	"context"
	"github.com/google/ts-bridge/datastore"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/appengine"
)

func setProjectID(projectID string) {
	if err := os.Setenv("GOOGLE_CLOUD_PROJECT", projectID); err != nil {
		fmt.Errorf("couldn't set env GOOGLE_CLOUD_PROJECT: %v", err)
	}
}

func TestNewConfigSimple(t *testing.T) {
	cfg, err := NewConfig(testCtx, &ConfigOptions{Filename: "testdata/valid.yaml", Storage: datastore.New()})
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.metrics) != 4 {
		t.Errorf("cfg.metrics expected to have 4 elements; got %v", cfg.metrics)
	}

	// 'testapp' is the default app id used by aetest.
	if cfg.StackdriverDestinations[0].ProjectID != "testapp" {
		t.Errorf("expected destination project to be equal to app id; got %v", cfg.StackdriverDestinations[0].ProjectID)
	}

	// project_id parameter is required when app id cannot be detected.
	for _, projectid := range []string{""} {
		setProjectID(projectid)
		_, err := NewConfig(testCtx, &ConfigOptions{Filename: "testdata/valid.yaml"})
		if !strings.Contains(err.Error(), "please provide project_id for") {
			t.Errorf("passing project_id should be required")
		}
	}

	// restore original test projectID
	setProjectID("testapp")
}

func TestNewConfigFailedValidation(t *testing.T) {
	for _, tt := range []struct {
		filename string
		wantErr  string
	}{
		{"duplicate_destinations.yaml", "file contains several destinations named"},
		{"duplicate_metrics.yaml", "duplicate metric name"},
		{"no_destination.yaml", "destination 'foo' not found"},
		{"no_datadog_keys.yaml", "configuration file validation error"},
		{"invalid_name.yaml", "configuration file validation error"},
		{"no_influxdb_query.yaml", "configuration file validation error"},
	} {
		_, err := NewConfig(testCtx, &ConfigOptions{Filename: filepath.Join("testdata", tt.filename), Storage: datastore.New()})
		if !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("expected NewConfig error '%v'; got '%v'", tt.wantErr, err)
		}
	}
}
