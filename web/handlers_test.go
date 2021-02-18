package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/ts-bridge/datastore"
	"github.com/google/ts-bridge/tasks"
	"github.com/google/ts-bridge/tsbridge"
	log "github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())
	// Save the emulator's quit channel.
	quit := datastore.Emulator(ctx, false)
	code := m.Run()
	cancel()
	// Wait for channel close before exiting the test suite
	<-quit
	os.Exit(code)
}

func TestHealthHandler(t *testing.T) {
	for _, storageEngineName := range []string{
		"boltdb",
		"datastore",
	} {
		t.Run(storageEngineName, func(t *testing.T) {
			config := tsbridge.NewConfig(&tsbridge.ConfigOptions{
				DatastoreProject: "testapp",
				Filename:         "testdata/valid.yaml",
				StorageEngine:    storageEngineName,
			})
			store, err := tasks.LoadStorageEngine(context.Background(), config)
			if err != nil {
				t.Fatalf("error while loading storage engine: %v", err)
			}

			metricCfg, err := tsbridge.NewMetricConfig(context.Background(), config, store)
			if err != nil {
				log.Fatalf("failed to perform initial load of metric config: %v", err)
			}

			h := NewHandler(config, &tsbridge.Metrics{}, metricCfg, store)

			req, err := http.NewRequest("GET", "/health", nil)
			if err != nil {
				t.Fatalf("error while sending request for /health: %v", err)
			}

			rr := httptest.NewRecorder()
			adapter := http.HandlerFunc(h.Health)
			adapter.ServeHTTP(rr, req)

			if status := rr.Code; status != http.StatusOK {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, http.StatusOK)
			}

			expected := `{"status":"ok"}`
			if rr.Body.String() != expected {
				t.Errorf("handler returned unexpected body: got %v want %v",
					rr.Body.String(), expected)
			}
		})
	}
}
