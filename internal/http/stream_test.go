package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	portyop "github.com/msoldin/porty/internal/operation"
	portyws "github.com/msoldin/porty/internal/websocket"
)

func TestAuthenticatedWebSocketSubscriptionStreamsSequencedEvents(t *testing.T) {
	hub := portyws.NewHub(8)
	handler, sessionCookie, _ := authenticatedAPIRouter(t, RouterOptions{Stream: portyws.Handler{Hub: hub}})
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/stream", &coderws.DialOptions{HTTPHeader: stdhttp.Header{
		"Origin": []string{server.URL}, "Cookie": []string{sessionCookie.String()},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	if err := wsjson.Write(ctx, connection, map[string]any{"type": "subscribe", "subscriptionId": "ops", "topic": "operations", "since": 0}); err != nil {
		t.Fatal(err)
	}
	hub.PublishOperation(portyop.Operation{ID: "op_1", Status: portyop.OperationRunning})
	var message struct {
		Type           string         `json:"type"`
		SubscriptionID string         `json:"subscriptionId"`
		Sequence       uint64         `json:"sequence"`
		Payload        map[string]any `json:"payload"`
	}
	if err := wsjson.Read(ctx, connection, &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != "operation" || message.SubscriptionID != "ops" || message.Sequence != 1 || message.Payload["id"] != "op_1" {
		t.Fatalf("message = %#v", message)
	}
}

func TestStreamRequiresRepositorySetupBeforeUpgrade(t *testing.T) {
	called := false
	stream := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		called = true
		w.WriteHeader(stdhttp.StatusNoContent)
	})
	handler, sessionCookie, _ := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: notReadyRepositorySetup(), Stream: stream})
	request := httptest.NewRequest(stdhttp.MethodGet, "http://porty.local/api/v1/stream", nil)
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, stdhttp.StatusConflict, "RepositorySetupRequired")
	if called {
		t.Fatal("stream upgraded before repository setup")
	}
}
