package http

import (
	portystack "github.com/msoldin/porty/internal/stack"
	stdhttp "net/http"
)

func registerEnvironmentRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	if options.Environment == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/stacks/{id}/environment", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		keys, err := options.Environment.EnvironmentKeys(r.Context(), portystack.StackID(r.PathValue("id")))
		writeResult(w, r, map[string]any{"keys": keys}, err, stdhttp.StatusOK)
	}))
	mux.HandleFunc("PUT /api/v1/stacks/{id}/environment/{key}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var input struct {
			Value string `json:"value"`
		}
		if decodeBody(w, r, &input) != nil {
			return
		}
		if err := options.Environment.SetEnvironment(r.Context(), portystack.StackID(r.PathValue("id")), r.PathValue("key"), input.Value); err != nil {
			writeAPIError(w, r, err)
			return
		}
		w.WriteHeader(stdhttp.StatusNoContent)
	}))
	mux.HandleFunc("DELETE /api/v1/stacks/{id}/environment/{key}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if err := options.Environment.DeleteEnvironment(r.Context(), portystack.StackID(r.PathValue("id")), r.PathValue("key")); err != nil {
			writeAPIError(w, r, err)
			return
		}
		w.WriteHeader(stdhttp.StatusNoContent)
	}))
}
