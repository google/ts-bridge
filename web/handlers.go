// Package web contains code related to handling WebUI and API handlers used in AppEngine.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/google/ts-bridge/env"
	"github.com/google/ts-bridge/storage"
	"github.com/google/ts-bridge/tasks"
	"github.com/google/ts-bridge/tsbridge"

	"github.com/dustin/go-humanize"
	log "github.com/sirupsen/logrus"
)

type Handler struct {
	config    *tsbridge.Config
	Metrics   *tsbridge.Metrics
	metricCfg *tsbridge.MetricConfig
	store     storage.Manager
}

type HealthResponse struct {
	Status string `json:"status,omitempty"`
}

func NewHandler(config *tsbridge.Config, metrics *tsbridge.Metrics, metricCfg *tsbridge.MetricConfig, store storage.Manager) *Handler {
	return &Handler{config: config, Metrics: metrics, store: store, metricCfg: metricCfg}
}

// Sync is an HTTP wrapper around sync() method that is designed to be triggered by App Engine Cron.
func (h *Handler) Sync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, cancel := context.WithTimeout(ctx, h.config.Options.UpdateTimeout)
	defer cancel()

	if env.IsAppEngine() && r.Header.Get("X-Appengine-Cron") != "true" {
		http.Error(w, "Only cron requests are allowed here", http.StatusUnauthorized)
		return
	}

	if err := tasks.SyncMetrics(ctx, h.config, h.Metrics, h.metricCfg); err != nil {
		logAndReturnError(ctx, w, err)
	}
}

// Cleanup is an HTTP wrapper around cleanup() method that is designed to be triggered by App Engine Cron.
func (h *Handler) Cleanup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if env.IsAppEngine() && r.Header.Get("X-Appengine-Cron") != "true" {
		http.Error(w, "Only cron requests are allowed here", http.StatusUnauthorized)
		return
	}

	if err := tasks.Cleanup(ctx, h.config, h.store); err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
}

// Health is a simple healthcheck endpoint.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "This endpoint supports only GET and HEAD requests", http.StatusMethodNotAllowed)
		return
	}

	response, err := json.Marshal(HealthResponse{"ok"})
	if err != nil {
		logAndReturnError(ctx, w, fmt.Errorf("failed to marshal health check response into JSON: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

// TODO: this needs to be removed in favour of Embed.FS when it ships, see:
// https://github.com/golang/go/issues/41191
// https://go.googlesource.com/proposal/+/master/design/draft-embed.md
// Generate static assets needed for index to be backed into executable
//go:generate go-bindata -nometadata -o static_data.go -pkg web static/

// Index shows a web page with metric import status.
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	if h.config.Options.EnableStatusPage != true {
		http.Error(w, "Status page is disabled. Please set ENABLE_STATUS_PAGE or --enable-status-page flag to to enable it.",
			http.StatusNotFound)
		return
	}

	ctx := r.Context()

	metrics, err := tsbridge.NewMetricConfig(ctx, h.config, h.store)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}

	index, err := Asset("static/index.html")
	if err != nil {
		log.Fatalf("Unable to load static assets needed for WebUI: %v", err)
	}

	funcMap := template.FuncMap{"humantime": humanize.Time}
	t, err := template.New("index.html").Funcs(funcMap).Parse(string(index))
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	if err := t.Execute(w, metrics.Metrics()); err != nil {
		logAndReturnError(ctx, w, err)
	}
}

// logAndReturnError is a wrapper around http errors which logs errors with their associated context before returning.
//   This is needed since some URLs are triggered by App Engine cron, and error messages returned in HTTP response might
//   not be visible to humans, so we need to log them as well.
func logAndReturnError(ctx context.Context, w http.ResponseWriter, err error) {
	log.WithContext(ctx).WithError(err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
