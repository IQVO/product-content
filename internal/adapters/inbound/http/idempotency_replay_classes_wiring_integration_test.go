//go:build integration

// Rendered from warehouse-harness-template
// templates/idempotency/replay_classes_wiring_test.go.tmpl: the repo-specific
// half of the idempotency replay-class test. The key row and the writes live in
// the request's own transaction over real Postgres (the design of
// postgres/idempotency.go), so the closures run over a testcontainers pool:
// Releasing is the rollback, so KeyRows 0 and Committed 0 are real proofs. The
// pool comes from the package's shared helper by type inference, so this file
// names no driver package (the closure shape the fleet sensor expects).
package http_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	httpadapter "github.com/IQVO/product-content/internal/adapters/inbound/http"
	"github.com/IQVO/product-content/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-content/internal/adapters/outbound/postgres/pgtx"
	"github.com/IQVO/product-content/internal/testing/pgtest"
)

// newReplayOps returns the six operations over ONE clean store (a fresh
// testcontainers database per call).
func newReplayOps(t *testing.T) replayOps {
	t.Helper()
	pool := pgtest.NewPool(t)
	count := func(t *testing.T, query string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := pool.Exec(t.Context(), `CREATE TABLE replay_probe (tag text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	errs := map[string]error{}
	for _, row := range httpadapter.Catalogue() {
		errs[row.Slug] = row.Err
	}
	return replayOps{
		Catalogue: func() []problemEntry {
			var out []problemEntry
			for _, row := range httpadapter.Catalogue() {
				out = append(out, problemEntry{Slug: row.Slug, Status: row.Status, Class: row.Class})
			}
			return out
		},
		Middleware: func(_ *testing.T, next http.Handler) http.Handler {
			return httpadapter.RequireIdempotencyKey(postgres.NewIdempotencyStore(pool), wallClock{})(next)
		},
		// Refuse goes through the production error writer, so the class the
		// catalogue entry declares is what marks the capturing writer.
		Refuse: func(w http.ResponseWriter, r *http.Request, e problemEntry) {
			httpadapter.WriteError(w, r, errs[e.Slug])
		},
		// Write is one write through the transaction the middleware bound into
		// the request's context: the same unit of work a handler's repositories
		// join. pgtx.From's result is used without naming its type, so no
		// driver import is needed here.
		Write: func(ctx context.Context, tag string) error {
			tx, ok := pgtx.From(ctx)
			if !ok {
				return fmt.Errorf("the middleware did not bind a transaction into the request context")
			}
			_, err := tx.Exec(ctx, `INSERT INTO replay_probe (tag) VALUES ($1)`, tag)
			return err
		},
		Committed: func(t *testing.T, tag string) int {
			t.Helper()
			return count(t, `SELECT count(*) FROM replay_probe WHERE tag = $1`, tag)
		},
		KeyRows: func(t *testing.T, key string) int {
			t.Helper()
			return count(t, `SELECT count(*) FROM idempotency_keys WHERE route = '' AND key = $1`, key)
		},
	}
}
