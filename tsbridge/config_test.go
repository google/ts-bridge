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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/appengine"
)

func fakeAppIDFunc(app string) func(context.Context) string {
	return func(ctx context.Context) string {
		return app
	}
}

func TestNewConfigSimple(t *testing.T) {
	cfg, err := NewConfig(testCtx, "testdata/valid.yaml", time.Second)
	if err != nil {
		t.Error(err)
	}

	if len(cfg.metrics) != 2 {
		t.Errorf("cfg.metrics expected to have 2 elements; got %v", cfg.metrics)
	}

	// 'testapp' is the default app id used by aetest.
	if cfg.StackdriverDestinations[0].ProjectID != "testapp" {
		t.Errorf("expected destination project to be equal to app id; got %v", cfg.StackdriverDestinations[0].ProjectID)
	}

	// project_id parameter is required when app id cannot be detected.
	for _, appid := range []string{"", "None"} {
		appIDFunc = fakeAppIDFunc(appid)
		_, err := NewConfig(testCtx, "testdata/valid.yaml", time.Second)
		if !strings.Contains(err.Error(), "please provide project_id for") {
			t.Errorf("passing project_id should be required")
		}
	}

	// restore original appIDFunc.
	appIDFunc = appengine.AppID
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
	} {
		_, err := NewConfig(testCtx, filepath.Join("testdata", tt.filename), time.Second)
		if !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("expected NewConfig error '%v'; got '%v'", tt.wantErr, err)
		}
	}
}
