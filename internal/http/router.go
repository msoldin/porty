package http

import (
	"context"
	"github.com/msoldin/porty/internal/http/middleware"
	stdhttp "net/http"
)

func NewMux(readiness func(context.Context) error) stdhttp.Handler {
	return NewRouter(RouterOptions{Readiness: readiness})
}

func NewRouter(options RouterOptions) stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if err := options.Readiness(r.Context()); err != nil {
			WriteError(w, r, stdhttp.StatusServiceUnavailable, "NotReady", "Porty is not ready", nil)
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]string{"status": "ready"})
	})
	if options.Auth != nil {
		registerAuthRoutes(mux, options)
		registerAPIRoutes(mux, options)
	}
	return middleware.AssignRequestID(middleware.AccessLog(options.AccessLogger, mux))
}
