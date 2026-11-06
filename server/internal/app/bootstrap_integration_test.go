package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/streamero-app/streamero/server/internal/config"
	"github.com/streamero-app/streamero/server/internal/db"
)

// Integration test: requires a real Postgres (pgvector image) and Valkey.
// Set STREAMERO_TEST_DB_URL and STREAMERO_TEST_VALKEY_URL (CI does).
func TestBootstrapIntegration(t *testing.T) {
	dbURL, vkURL := os.Getenv("STREAMERO_TEST_DB_URL"), os.Getenv("STREAMERO_TEST_VALKEY_URL")
	if dbURL == "" || vkURL == "" {
		t.Skip("STREAMERO_TEST_DB_URL / STREAMERO_TEST_VALKEY_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{DatabaseURL: dbURL, ValkeyURL: vkURL, AutoMigrate: true}

	start := time.Now()
	state := &State{}
	deps, err := Bootstrap(ctx, cfg, state, log)
	if err != nil {
		t.Fatal(err)
	}
	defer deps.Close()
	if !state.Ready() {
		t.Fatal("not ready after bootstrap")
	}
	pending, err := db.Pending(ctx, deps.Pool)
	if err != nil || pending {
		t.Fatalf("pending=%v err=%v", pending, err)
	}

	// Second start without migrations must keep the same server ID and be fast.
	cfg.AutoMigrate = false
	state2 := &State{}
	start = time.Now()
	deps2, err := Bootstrap(ctx, cfg, state2, log)
	if err != nil {
		t.Fatal(err)
	}
	defer deps2.Close()
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("startup to ready took %s, budget 5s", d)
	}
	if state.ServerID() != state2.ServerID() {
		t.Errorf("server id changed: %s != %s", state.ServerID(), state2.ServerID())
	}
}
