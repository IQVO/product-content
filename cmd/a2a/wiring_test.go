package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestBuildDepsRunsMigrationsOnMigrationsDatabaseURL proves buildDeps itself,
// not just the env lookup in run(), sends the boot migrations to the direct
// MIGRATIONS_DATABASE_URL and the application pool to DATABASE_URL: the two are
// never conflated (a PgBouncer in transaction mode breaks the session-scoped
// advisory lock of golang-migrate).
//
// databaseURL points at an address nothing listens on, so reaching the pool (or
// running the migrations against it) would show a dial error; the migrations
// URL is a schemeless string golang-migrate rejects immediately with a
// distinctive parse error. The short context ends the retry backoff, and the
// proof is the logged "retrying ... err=" line, because the returned error only
// wraps the cancelled context.
func TestBuildDepsRunsMigrationsOnMigrationsDatabaseURL(t *testing.T) {
	const (
		bogusMigrationsURL = "not-a-valid-connection-string"
		unreachableAppURL  = "postgres://u@127.0.0.1:1/db?sslmode=disable&connect_timeout=1"
	)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_, closeAdapters, err := buildDeps(ctx, logger, unreachableAppURL, bogusMigrationsURL)
	defer closeAdapters()
	if err == nil {
		t.Fatal("a malformed MIGRATIONS_DATABASE_URL must fail boot")
	}
	out := logs.String()
	if !strings.Contains(out, "retrying") || !strings.Contains(out, "run migrations") {
		t.Fatalf("the migrations step did not run first: %q (err %v)", out, err)
	}
	if !strings.Contains(out, "parse scheme") {
		t.Fatalf("logs = %q: expected the bogus-URL parse error from the migrations step; a dial error would mean the migrations ran against DATABASE_URL", out)
	}
}
