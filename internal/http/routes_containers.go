package http

import (
	stdhttp "net/http"

	portystack "github.com/msoldin/porty/internal/stack"
)

func registerContainerRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	if options.Containers == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/stacks/{id}/containers", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		items, err := options.Containers.Containers(r.Context(), portystack.StackID(r.PathValue("id")))
		writeResult(w, r, items, err, stdhttp.StatusOK)
	}))
	mux.HandleFunc("POST /api/v1/stacks/{id}/containers/{containerId}/actions/{action}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		operation, err := options.Containers.StartContainerAction(r.Context(), portystack.StackID(r.PathValue("id")), r.PathValue("containerId"), r.PathValue("action"))
		writeResult(w, r, operation, err, stdhttp.StatusAccepted)
	}))
}
