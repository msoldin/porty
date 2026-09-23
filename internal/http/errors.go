package http

import (
	"encoding/json"
	"github.com/msoldin/porty/internal/http/middleware"
	stdhttp "net/http"
)

type ErrorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"requestId"`
	Details   map[string]any `json:"details,omitempty"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

func WriteError(w stdhttp.ResponseWriter, r *stdhttp.Request, status int, code, message string, details map[string]any) {
	writeJSON(w, status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: middleware.RequestID(r.Context()), Details: details,
	}})
}

func writeJSON(w stdhttp.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
