package http

import (
	"github.com/msoldin/porty/internal/autoupdate"
	"github.com/msoldin/porty/internal/stack"
	stdhttp "net/http"
)

func registerAutoUpdateRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	if options.AutoUpdates == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/stacks/{id}/auto-update", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		status, err := options.AutoUpdates.Get(r.Context(), stack.StackID(r.PathValue("id")))
		writeResult(w, r, status, err, stdhttp.StatusOK)
	}))
	mux.HandleFunc("PUT /api/v1/stacks/{id}/auto-update", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var input autoupdate.PolicyUpdate
		if decodeBody(w, r, &input) != nil {
			return
		}
		actor := principalFrom(r.Context()).UserID
		status, err := options.AutoUpdates.Update(r.Context(), stack.StackID(r.PathValue("id")), input, actor)
		if err == nil {
			recordAudit(options, r, actor, "auto_update.policy", "stack", r.PathValue("id"), "succeeded")
		}
		writeResult(w, r, status, err, stdhttp.StatusOK)
	}))
	mux.HandleFunc("POST /api/v1/stacks/{id}/auto-update/resume", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var input struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if decodeBody(w, r, &input) != nil {
			return
		}
		actor := principalFrom(r.Context()).UserID
		status, err := options.AutoUpdates.Resume(r.Context(), stack.StackID(r.PathValue("id")), input.ExpectedRevision, actor)
		if err == nil {
			recordAudit(options, r, actor, "auto_update.resume", "stack", r.PathValue("id"), "succeeded")
		}
		writeResult(w, r, status, err, stdhttp.StatusOK)
	}))
}
