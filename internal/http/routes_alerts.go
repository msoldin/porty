package http

import (
	"github.com/msoldin/porty/internal/alert"
	stdhttp "net/http"
	"strconv"
)

func registerAlertRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	if options.Alerts == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/alerts", authenticatedRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		limit, offset, err := alertPagination(r)
		if err != nil {
			writeAPIError(w, r, err)
			return
		}
		filter, err := alert.NormalizeFilter(alert.Filter{StackID: r.URL.Query().Get("stackId"), View: r.URL.Query().Get("view"), Limit: limit, Offset: offset})
		if err != nil {
			writeAPIError(w, r, err)
			return
		}
		page, err := options.Alerts.List(r.Context(), filter)
		writeResult(w, r, page, err, stdhttp.StatusOK)
	}))
	mux.HandleFunc("GET /api/v1/alerts/{id}", authenticatedRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		limit, offset, err := alertPagination(r)
		if err != nil {
			writeAPIError(w, r, err)
			return
		}
		item, err := options.Alerts.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			writeAPIError(w, r, err)
			return
		}
		events, err := options.Alerts.History(r.Context(), item.ID, limit, offset)
		writeResult(w, r, struct {
			Alert   alert.Alert   `json:"alert"`
			History []alert.Event `json:"history"`
		}{item, events}, err, stdhttp.StatusOK)
	}))
	mux.HandleFunc("POST /api/v1/alerts/{id}/acknowledge", mutationAuthRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var input struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if decodeBody(w, r, &input) != nil {
			return
		}
		item, err := options.Alerts.Acknowledge(r.Context(), alert.Mutation{ID: r.PathValue("id"), ExpectedRevision: input.ExpectedRevision, ActorID: principalFrom(r.Context()).UserID})
		if err == nil {
			recordAudit(options, r, principalFrom(r.Context()).UserID, "alert.acknowledge", "alert", item.ID, "succeeded")
		}
		writeResult(w, r, item, err, stdhttp.StatusOK)
	}))
	mux.HandleFunc("POST /api/v1/alerts/{id}/resolve", mutationAuthRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var input struct {
			ExpectedRevision int64  `json:"expectedRevision"`
			Note             string `json:"note"`
		}
		if decodeBody(w, r, &input) != nil {
			return
		}
		item, err := options.Alerts.Resolve(r.Context(), alert.Mutation{ID: r.PathValue("id"), ExpectedRevision: input.ExpectedRevision, Note: input.Note, ActorID: principalFrom(r.Context()).UserID})
		if err == nil {
			recordAudit(options, r, principalFrom(r.Context()).UserID, "alert.resolve", "alert", item.ID, "succeeded")
		}
		writeResult(w, r, item, err, stdhttp.StatusOK)
	}))
}

func alertPagination(r *stdhttp.Request) (int, int, error) {
	limit, offset := 50, 0
	for name, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if raw, ok := r.URL.Query()[name]; ok {
			if len(raw) != 1 {
				return 0, 0, alert.ErrInvalid
			}
			value, err := strconv.Atoi(raw[0])
			if err != nil || value < 0 || (name == "limit" && value == 0) {
				return 0, 0, alert.ErrInvalid
			}
			*target = value
		}
	}
	if limit > 200 {
		limit = 200
	}
	return limit, offset, nil
}
