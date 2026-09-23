package sqlite

import (
	"context"
	"database/sql"
	"errors"
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

func TestStackReadsPreserveOrderingArchiveAndMissingRows(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "stacks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStackStore(db)
	now := time.Now().UTC()
	for _, stack := range []domain.Stack{
		{ID: "stk_z", DirectoryName: "zulu", ComposeProjectName: "porty-zulu", CreatedAt: now},
		{ID: "stk_a", DirectoryName: "alpha", ComposeProjectName: "porty-alpha", CreatedAt: now},
		{ID: "stk_m", DirectoryName: "middle", ComposeProjectName: "porty-middle", CreatedAt: now},
	} {
		if err := store.Create(ctx, stack); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Archive(ctx, "stk_m", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	active, err := store.Active(ctx)
	if err != nil || len(active) != 2 || active[0].ID != "stk_a" || active[1].ID != "stk_z" {
		t.Fatalf("active=%#v err=%v", active, err)
	}
	archived, err := store.ByDirectory(ctx, "middle")
	if err != nil || archived.ArchivedAt == nil {
		t.Fatalf("archived=%#v err=%v", archived, err)
	}
	if _, err := store.ByID(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing row: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Active(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled list: %v", err)
	}
}
