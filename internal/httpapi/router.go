package httpapi

import (
	"context"
	"net/http"
)

func NewMux(readiness func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := readiness(r.Context()); err != nil {
			WriteError(w, r, http.StatusServiceUnavailable, "NotReady", "Porty is not ready", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	return requestIDMiddleware(mux)
}
