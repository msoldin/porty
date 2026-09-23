package http

import (
	"context"
	portyop "github.com/msoldin/porty/internal/operation"
	portystack "github.com/msoldin/porty/internal/stack"
	stdhttp "net/http"
)

func registerOperationRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	if options.Operations != nil {
		mux.HandleFunc("GET /api/v1/operations", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var value []portyop.Operation
			var err error
			if paged, ok := options.Operations.(interface {
				OperationsPage(context.Context, int, int) ([]portyop.Operation, error)
			}); ok {
				value, err = paged.OperationsPage(r.Context(), queryLimit(r), queryOffset(r))
			} else {
				value, err = options.Operations.Operations(r.Context(), queryLimit(r))
			}
			writeResult(w, r, value, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("GET /api/v1/operations/{id}", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			value, err := options.Operations.Operation(r.Context(), r.PathValue("id"))
			writeResult(w, r, value, err, stdhttp.StatusOK)
		}))
	}
	if options.Deployments != nil {
		mux.HandleFunc("GET /api/v1/stacks/{id}/deployments", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			id := portystack.StackID(r.PathValue("id"))
			var value []portyop.Deployment
			var err error
			if paged, ok := options.Deployments.(interface {
				DeploymentsPage(context.Context, portystack.StackID, int, int) ([]portyop.Deployment, error)
			}); ok {
				value, err = paged.DeploymentsPage(r.Context(), id, queryLimit(r), queryOffset(r))
			} else {
				value, err = options.Deployments.Deployments(r.Context(), id, queryLimit(r))
			}
			writeResult(w, r, value, err, stdhttp.StatusOK)
		}))
	}
}
