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
	"os"
	"testing"

	"google.golang.org/appengine"
	"google.golang.org/appengine/aetest"
)

// common context shared between all tests. Otherwise each test would need to start a separate
// instance of dev_appserver.py which is quite slow.
var testCtx context.Context

func TestMain(m *testing.M) {
	// Use strongly consistent datastore in tests to verify metric record cleanup.
	inst, err := aetest.NewInstance(&aetest.Options{StronglyConsistentDatastore: true})
	if err != nil {
		panic(err)
	}
	req, err := inst.NewRequest("GET", "/", nil)
	if err != nil {
		panic(err)
	}
	testCtx = appengine.NewContext(req)

	code := m.Run()
	inst.Close()
	os.Exit(code)
}
