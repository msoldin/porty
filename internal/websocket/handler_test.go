package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
)

func TestIdleConnectionClosesAtAccessDeadline(t *testing.T) {
	hub := NewHub(8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
		defer cancel()
		Handler{Hub: hub}.ServeHTTP(w, r.WithContext(ctx))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	if _, _, err := connection.Read(ctx); err == nil {
		t.Fatal("idle stream stayed open beyond deadline")
	}
	if ctx.Err() != nil {
		t.Fatal("stream did not close before test timeout")
	}
}

func TestKeyRotationClosesIdleConnection(t *testing.T) {
	hub := NewHub(8)
	server := httptest.NewServer(Handler{Hub: hub})
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	for {
		hub.mu.Lock()
		connected := len(hub.connections) == 1
		hub.mu.Unlock()
		if connected {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("connection was not registered")
		}
		time.Sleep(time.Millisecond)
	}
	hub.CloseConnections()
	if _, _, err := connection.Read(ctx); err == nil {
		t.Fatal("idle stream stayed open after key rotation")
	}
	if ctx.Err() != nil {
		t.Fatal("stream did not close before test timeout")
	}
}
