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
	"io/ioutil"
	"net"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"os"
	"os/exec"
	"syscall"
)

// Fake projectID set by the emulator
const fakeProjectID = "testapp"

// Emulator instantiates datastore emulator for use in tests.
//
// Background: this was devised due to aetest package no longer being available for 1.12+ runtimes as dev_appserver.py
// cannot run runtimes past go111, see: https://github.com/googleapis/google-cloud-go/issues/644
//
//   Usage:
//
//	 ctx, cancel := context.WithCancel(context.Background())
//	 // Save the emulator's quit channel.
//	 quit := Emulator(ctx, true)
//	 code := m.Run()
//	 cancel()
//	 // Wait for channel close
//	 <-quit
//
func Emulator(ctx context.Context, setGAE bool) <-chan struct{} {

	// Create a child logger and set it to process newlines since emulator gives a lot of output.
	l := log.WithContext(ctx)
	l.Logger.SetFormatter(&log.TextFormatter{DisableQuote: true})

	// Get the actual Emulator binary path.
	emuExec := findEmulatorPath()
	port := fetchPort()

	// Set up tempdir to use the emulator with.
	tempDir, err := ioutil.TempDir("", "dsemu")
	if err != nil {
		log.Fatalf("Unable to create tempdir %v:", err)
	}

	var out bytes.Buffer
	cmd := exec.Command(emuExec, "start",
		// Start in 'testing' mode. Equivalent to passing --consistency=1.0,
		//   --store_on_disk=false,
		//   --store_index_configuration_on_disk=false
		"--testing",
		"--host=0.0.0.0",
		fmt.Sprintf("--port=%v", port),
		tempDir,
	)
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // make this process a new group leader to help cleanup

	quit := make(chan struct{})

	log.Infof("booting up datastore emulator: %v", cmd.Args)
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
		// Stop emulator goroutine when context is finished by sending SIGINT to the whole process group
		case <-ctx.Done():
			syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)

		case <-quit:
		}
	}()

	if err := os.Setenv("DATASTORE_EMULATOR_HOST", fmt.Sprintf("localhost:%v", port)); err != nil {
		log.Errorf("couldn't set env DATASTORE_EMULATOR_HOST: %v", err)
	}

	// This is emulating the old `aetest` framework that has a hardcoded "testapp" value for project
	if err := os.Setenv("GOOGLE_CLOUD_PROJECT", fakeProjectID); err != nil {
		log.Fatalf("couldn't set env GOOGLE_CLOUD_PROJECT: %v", err)
	}

	// This env is set to pretend we're running on AppEngine
	if setGAE {
		if err := os.Setenv("GAE_ENV", "standard"); err != nil {
			log.Fatalf("couldn't set env GAE_ENV: %v", err)
		}
	}

	return quit
}

func findEmulatorPath() string {
	path, err := exec.LookPath("gcloud")
	if err != nil {
		log.Fatalf("Couldn't determine gcloud path: %v", err)
	}

	return strings.Replace(path, "bin/gcloud", "platform/cloud-datastore-emulator/cloud_datastore_emulator", 1)
}

func fetchPort() int {
	// Get a random port from the system
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("Unable to resolve local address to fetch port: %v", addr)
	}

	// Ensure we can bind to it
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatalf("unable to open socket: %v", err)
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}
