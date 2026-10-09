package http

import (
	"encoding/json"
	"errors"
	"github.com/msoldin/porty/internal/monitoring"
	"net/http"
	"net/url"
)

type MonitoringAPI interface {
	Snapshot(cursor string) (monitoring.Snapshot, error)
}

func registerMonitoringRoutes(mux *http.ServeMux, options RouterOptions) {
	if options.Monitoring == nil {
		return
	}
	handler := authenticatedRoute(options, func(w http.ResponseWriter, r *http.Request) {
		query, err := url.ParseQuery(r.URL.RawQuery)
		invalid := err != nil || len(r.URL.RawQuery) > 512 || len(query) > 1
		for key, values := range query {
			if key != "cursor" || len(values) != 1 || len(values[0]) > 256 {
				invalid = true
			}
		}
		if invalid {
			WriteError(w, r, 400, "InvalidCursor", "Monitoring cursor is invalid", nil)
			return
		}
		snapshot, err := options.Monitoring.Snapshot(query.Get("cursor"))
		if errors.Is(err, monitoring.ErrInvalidCursor) {
			WriteError(w, r, 400, "InvalidCursor", "Monitoring cursor is invalid", nil)
			return
		}
		if err != nil {
			WriteError(w, r, 500, "MonitoringUnavailable", "Monitoring snapshot is unavailable", nil)
			return
		}
		body, err := json.Marshal(snapshot)
		if err != nil || len(body) > 8<<20 {
			WriteError(w, r, 500, "MonitoringUnavailable", "Monitoring snapshot is unavailable", nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	mux.HandleFunc("GET /api/v1/monitoring", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		handler(w, r)
	})
}
