// Package web contains code related to handling WebUI and API handlers used in AppEngine.
package web

import (
	"context"
	"github.com/google/ts-bridge/tasks"
	log "github.com/sirupsen/logrus"
	"html/template"
	"net/http"

	"github.com/dustin/go-humanize"
	"github.com/google/ts-bridge/env"
	"github.com/google/ts-bridge/tsbridge"
)

type Handler struct {
	config *tsbridge.Config
}

func NewHandler(config *tsbridge.Config) *Handler {
	return &Handler{config: config}
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

	if err := tasks.Sync(ctx, h.config); err != nil {
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

	if err := tasks.Cleanup(ctx, h.config); err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
}

// Index shows a web page with metric import status.
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	if h.config.Options.EnableStatusPage != true {
		http.Error(w, "Status page is disabled. Please set ENABLE_STATUS_PAGE or --enable-status-page flag to to enable it.",
			http.StatusNotFound)
		return
	}

	ctx := r.Context()

	storage, err := tasks.LoadStorageEngine(ctx, h.config)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}
	defer storage.Close()

	metrics, err := tsbridge.NewMetricConfig(ctx, h.config, storage)
	if err != nil {
		logAndReturnError(ctx, w, err)
		return
	}

	funcMap := template.FuncMap{"humantime": humanize.Time}
	t, err := template.New("index.html").Funcs(funcMap).ParseFiles("web/static/index.html")
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
