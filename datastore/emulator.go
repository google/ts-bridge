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

package datastore

import (
	"bytes"
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"time"

	"os"
	"os/exec"
	"syscall"
)

// Fake projectID set by the emulator
const fakeProjectID = "testapp"

// Port for emulator to use
var emulatorPort = 30801

// Emulator instantiates datastore emulator for use in tests.
//
// Background: this was devised due to aetest package no longer being available for 1.12+ runtimes as dev_appserver.py
// cannot run runtimes past go111, see: https://github.com/googleapis/google-cloud-go/issues/644
//
//   Usage:
//
//	 ctx, cancel := context.WithCancel(context.Background())
//	 // Save the emulator's quit channel.
//	 quit := Emulator(ctx)
//	 code := m.Run()
//	 cancel()
//	 // Wait for channel close
//	 <-quit
//
func Emulator(ctx context.Context) <-chan struct{} {
	l := log.WithContext(ctx)

	// Set log formatter to process newlines since emulator gives a lot of output
	l.Logger.SetFormatter(&log.TextFormatter{DisableQuote: true})

	// Set up the command
	var out bytes.Buffer
	// --in-memory flag is important as DS emulator doesn't allow reset if data is stored on disk
	cmd := exec.Command("gcloud",
		"beta", "emulators", "datastore", "start",
		fmt.Sprintf("--host-port=0.0.0.0:%v", emulatorPort),
		"--no-store-on-disk",       // use in-memory store
		"--consistency=1.0",        // disable random failure injection
		"--project="+fakeProjectID, // use a fake project id so it doesn't try to infer one from environment
	)
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // make this process a new group leader to help cleanup

	quit := make(chan struct{})

	log.Info("booting up datastore emulator...")
	if err := cmd.Start(); err != nil {
		log.Fatalf("unable to start DS emulator: %v\n%v", err, out.String())
	}
	time.Sleep(2 * time.Second)

	go func() {
		defer close(quit)
		if err := cmd.Wait(); err != nil {
			log.Infof("Datastore emulator killed: %v\n\t%v", err, out.String())
		}
		log.Info("Datastore emulator exited.")
	}()

	go func() {
		select {
		// Stop datastore emulator goroutine when context is finished
		case <-ctx.Done():
			cmd.Process.Signal(os.Interrupt)

		case <-quit:
		}
	}()

	if err := os.Setenv("DATASTORE_EMULATOR_HOST", fmt.Sprintf("localhost:%v", emulatorPort)); err != nil {
		log.Errorf("couldn't set env DATASTORE_EMULATOR_HOST: %v", err)
	}

	// This is emulating the old `aetest` framework that has a hardcoded "testapp" value for project
	if err := os.Setenv("GOOGLE_CLOUD_PROJECT", fmt.Sprintf(fakeProjectID)); err != nil {
		log.Errorf("couldn't set env GOOGLE_CLOUD_PROJECT: %v", err)
	}

	return quit
}
