package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/ts-bridge/tasks"
	"github.com/google/ts-bridge/tsbridge"
)

func TestHealthHandler(t *testing.T) {
	for _, storageEngineName := range []string{
		"boltdb",
		"datastore",
	} {
		t.Run(storageEngineName, func(t *testing.T) {
			config := tsbridge.NewConfig(&tsbridge.ConfigOptions{
				StorageEngine:    storageEngineName,
				DatastoreProject: "testapp",
			})
			store, err := tasks.LoadStorageEngine(context.Background(), config)
			if err != nil {
				t.Fatalf("Error while loading storage engine: %v", err)
			}
			h := NewHandler(config, &tsbridge.Metrics{}, store)

			req, err := http.NewRequest("GET", "/health", nil)
			if err != nil {
				t.Fatalf("Error while sending request for /health: %v", err)
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
