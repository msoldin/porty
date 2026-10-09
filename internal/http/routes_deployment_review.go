package http

import (
	"encoding/hex"
	"errors"
	portycontrol "github.com/msoldin/porty/internal/control"
	portystack "github.com/msoldin/porty/internal/stack"
	stdhttp "net/http"
)

func registerDeploymentReviewRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	if options.DeploymentReview == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/stacks/{id}/deployment-review", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		value, err := options.DeploymentReview.ReviewDeployment(r.Context(), portystack.StackID(r.PathValue("id")))
		writeResult(w, r, value, err, stdhttp.StatusOK)
	}))
}

// A present precondition must never fall back to an unchecked deployment.
func handleReviewedDeployment(w stdhttp.ResponseWriter, r *stdhttp.Request, options RouterOptions) bool {
	headers, present := r.Header["If-Match"]
	if r.PathValue("action") != "deploy" || !present {
		return false
	}
	valid := len(headers) == 1
	revision := ""
	if valid {
		value := headers[0]
		valid = len(value) == 66 && value[0] == '"' && value[65] == '"'
		if valid {
			revision = value[1:65]
			_, err := hex.DecodeString(revision)
			valid = err == nil
		}
	}
	if !valid {
		WriteError(w, r, stdhttp.StatusBadRequest, "InvalidRequest", "Use one quoted deployment review revision", nil)
		return true
	}
	if options.DeploymentReview == nil {
		WriteError(w, r, stdhttp.StatusServiceUnavailable, "DeploymentReviewUnavailable", "Deployment review is unavailable", nil)
		return true
	}
	value, err := options.DeploymentReview.StartReviewedDeployment(r.Context(), portystack.StackID(r.PathValue("id")), revision)
	if errors.Is(err, portycontrol.ErrDeploymentReviewChanged) {
		WriteError(w, r, stdhttp.StatusPreconditionFailed, "DeploymentReviewChanged", "Saved configuration changed. Review it again before deploying.", nil)
		return true
	}
	writeResult(w, r, value, err, stdhttp.StatusAccepted)
	return true
}
