package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/ts-bridge/tsbridge"
)

func TestHealthHandler(t *testing.T) {
	config := tsbridge.NewConfig(&tsbridge.ConfigOptions{})
	h := NewHandler(config, &tsbridge.Metrics{})

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
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
}
