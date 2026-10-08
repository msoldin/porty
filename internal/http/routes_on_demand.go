package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"time"

	"github.com/msoldin/porty/internal/compose"
	"github.com/msoldin/porty/internal/ondemand"
	"github.com/msoldin/porty/internal/stack"
)

type OnDemandAPI interface {
	ListGroups(context.Context, stack.StackID) ([]ondemand.Group, error)
	SaveGroup(context.Context, stack.StackID, string, ondemand.PolicyUpdate) (ondemand.Group, error)
	DeleteGroup(context.Context, stack.StackID, string, int64) error
	HoldGroup(context.Context, stack.StackID, string, int64) error
	ResumeGroup(context.Context, stack.StackID, string, int64) (ondemand.Group, error)
}

func registerOnDemandRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	if options.OnDemand == nil {
		return
	}
	base := "/api/v1/stacks/{id}/on-demand"
	mux.HandleFunc("GET "+base, readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		groups, err := options.OnDemand.ListGroups(r.Context(), stack.StackID(r.PathValue("id")))
		writeOnDemandResult(w, r, groups, err, stdhttp.StatusOK)
	}))
	for _, method := range []string{"POST", "PUT"} {
		path := base
		if method == "PUT" {
			path += "/{group}"
		}
		mux.HandleFunc(method+" "+path, mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var input ondemand.PolicyUpdate
			if decodeBody(w, r, &input) != nil {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
			defer cancel()
			group, err := options.OnDemand.SaveGroup(ctx, stack.StackID(r.PathValue("id")), r.PathValue("group"), input)
			status := stdhttp.StatusOK
			if r.Method == "POST" {
				status = stdhttp.StatusCreated
			}
			writeOnDemandResult(w, r, group, err, status)
		}))
	}
	for _, action := range []string{"hold", "resume", "delete"} {
		pattern := "POST " + base + "/{group}/" + action
		if action == "delete" {
			pattern = "DELETE " + base + "/{group}"
		}
		mux.HandleFunc(pattern, mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var input struct {
				ExpectedRevision int64 `json:"expectedRevision"`
			}
			if decodeBody(w, r, &input) != nil {
				return
			}
			if input.ExpectedRevision < 1 {
				writeOnDemandResult(w, r, nil, ondemand.ErrConflict, 0)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
			defer cancel()
			id, group := stack.StackID(r.PathValue("id")), r.PathValue("group")
			var result any
			var err error
			status := stdhttp.StatusNoContent
			switch action {
			case "hold":
				err = options.OnDemand.HoldGroup(ctx, id, group, input.ExpectedRevision)
			case "resume":
				result, err = options.OnDemand.ResumeGroup(ctx, id, group, input.ExpectedRevision)
				status = stdhttp.StatusOK
			case "delete":
				err = options.OnDemand.DeleteGroup(ctx, id, group, input.ExpectedRevision)
			}
			writeOnDemandResult(w, r, result, err, status)
		}))
	}
}
func writeOnDemandResult(w stdhttp.ResponseWriter, r *stdhttp.Request, result any, err error, status int) {
	switch {
	case errors.Is(err, ondemand.ErrConflict):
		WriteError(w, r, 409, "OnDemandConflict", "The group or runtime changed. Refresh and review before retrying.", nil)
	case errors.Is(err, ondemand.ErrInvalid):
		WriteError(w, r, 400, "InvalidOnDemandPolicy", err.Error(), nil)
	case errors.Is(err, ondemand.ErrUnavailable), errors.Is(err, compose.ErrOnDemandIneligible), errors.Is(err, compose.ErrProtectionUnavailable), errors.Is(err, compose.ErrSelfProtected):
		WriteError(w, r, 409, "OnDemandUnavailable", "Requires a deployed, healthy group with fixed bridge-network ports and local rootful Docker. Porty must share the host network; its own stack is protected.", nil)
	case err != nil:
		writeAPIError(w, r, err)
	case status == stdhttp.StatusNoContent:
		w.WriteHeader(status)
	default:
		writeJSON(w, status, result)
	}
}
