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

	"github.com/google/ts-bridge/datastore"
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())
	// Save the emulator's quit channel.
	quit := datastore.Emulator(ctx, true)
	code := m.Run()
	cancel()
	// Wait for channel close before exiting the test suite
	<-quit
	os.Exit(code)
}
