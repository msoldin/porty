package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

func TestArchivedStackRetainsEnvironmentUntilExplicitPurge(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "stacks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStackStore(db)
	stack := domain.Stack{ID: "stk_gateway", DirectoryName: "gateway", ComposeProjectName: "porty-gateway-a1b2", CreatedAt: time.Now().UTC()}
	if err := store.Create(context.Background(), stack); err != nil {
		t.Fatal(err)
	}
	if err := store.SetEnvironment(context.Background(), stack.ID, "TOKEN", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.Archive(context.Background(), stack.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	values, err := store.Environment(context.Background(), stack.ID)
	if err != nil {
		t.Fatal(err)
	}
	if values["TOKEN"] != "secret" {
		t.Fatalf("archived environment = %#v", values)
	}
	if err := store.Purge(context.Background(), stack.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Environment(context.Background(), stack.ID); err == nil {
		t.Fatal("Environment() after purge error = nil")
	}
}
