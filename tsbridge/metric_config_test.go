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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/ts-bridge/datastore"
)

func setProjectID(projectID string) {
	if err := os.Setenv("GOOGLE_CLOUD_PROJECT", projectID); err != nil {
		fmt.Errorf("couldn't set env GOOGLE_CLOUD_PROJECT: %v", err)
	}
}

func TestNewMetricConfigSimple(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

	config := NewConfig(&ConfigOptions{Filename: "testdata/valid.yaml"})
	cfg, err := NewMetricConfig(ctx, config, storage)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.metrics) != 4 {
		t.Errorf("cfg.metrics expected to have 4 elements; got %v", cfg.metrics)
	}

	// 'testapp' is the default app id used by the emulator
	if cfg.StackdriverDestinations[0].ProjectID != "testapp" {
		t.Errorf("expected destination project to be equal to app id; got %v", cfg.StackdriverDestinations[0].ProjectID)
	}

	setProjectID("")
	_, errNoProject := NewMetricConfig(ctx, config, storage)

	if errNoProject == nil {
		t.Fatalf("NewMetricConfig should produce an error with empty project id")
	}

	if !strings.Contains(errNoProject.Error(), "please provide project_id for") {
		t.Errorf("NewMetricConfig should prompt for project_id if one cannot be inferred")
	}

	// restore original test projectID
	setProjectID("testapp")
}

func TestNewMetricConfigFailedValidation(t *testing.T) {
	ctx := context.Background()
	storage := datastore.New(ctx, &datastore.Options{})

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
		config := NewConfig(&ConfigOptions{Filename: filepath.Join("testdata", tt.filename)})
		_, err := NewMetricConfig(ctx, config, storage)
		if !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("expected NewConfig error '%v'; got '%v'", tt.wantErr, err)
		}
	}
}
