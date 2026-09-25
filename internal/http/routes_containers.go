package http

import (
	"bytes"
	"encoding/json"
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
	containerReadRoute := func(next stdhttp.HandlerFunc) stdhttp.HandlerFunc {
		protected := readRoute(options, next)
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("Cache-Control", "no-store")
			protected(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/stacks/{id}/containers/{containerId}/logs", containerReadRoute(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		logs, err := options.Containers.ContainerLogs(r.Context(), portystack.StackID(r.PathValue("id")), r.PathValue("containerId"))
		writeResult(w, r, logs, err, stdhttp.StatusOK)
	}))
	mux.HandleFunc("GET /api/v1/stacks/{id}/containers/{containerId}/inspect", containerReadRoute(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		inspect, err := options.Containers.ContainerInspect(r.Context(), portystack.StackID(r.PathValue("id")), r.PathValue("containerId"))
		if err != nil {
			writeAPIError(w, r, err)
			return
		}
		var formatted bytes.Buffer
		if err := json.Indent(&formatted, inspect, "", "  "); err != nil {
			WriteError(w, r, stdhttp.StatusBadGateway, "InvalidContainerInspect", "Docker returned invalid inspect JSON", nil)
			return
		}
		formatted.WriteByte('\n')
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write(formatted.Bytes())
	}))
	mux.HandleFunc("POST /api/v1/stacks/{id}/containers/{containerId}/actions/{action}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		operation, err := options.Containers.StartContainerAction(r.Context(), portystack.StackID(r.PathValue("id")), r.PathValue("containerId"), r.PathValue("action"))
		writeResult(w, r, operation, err, stdhttp.StatusAccepted)
	}))
	mux.HandleFunc("POST /api/v1/stacks/{id}/containers/actions/{action}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var input struct {
			ContainerIDs []string `json:"containerIds"`
		}
		if decodeBody(w, r, &input) != nil {
			return
		}
		operation, err := options.Containers.StartContainerBatchAction(r.Context(), portystack.StackID(r.PathValue("id")), input.ContainerIDs, r.PathValue("action"))
		writeResult(w, r, operation, err, stdhttp.StatusAccepted)
	}))
}
