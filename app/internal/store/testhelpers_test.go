package store

import (
	"context"
	"os"
	"testing"
)

// newTestStore opens (or reuses) a real Postgres connection and resets
// every table this package's tests touch to empty before returning —
// this package has no mock/fake DB layer (raw pgx/v5 SQL throughout, see
// package doc comment), so these tests run against the real thing, same
// as every other dbtx-taking helper in this package already assumes a
// real Postgres connection with real range/array/enum types.
//
// Point SONGBOOKS_TEST_DATABASE_URL at a scratch database to run these;
// they're skipped otherwise rather than failing a build that has no
// Postgres available (CI without a configured test DB, a sandboxed dev
// environment, ...).
func newTestStore(t *testing.T) *Store {
	t.Helper()
	connString := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if connString == "" {
		connString = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	ctx := context.Background()
	st, err := New(ctx, connString)
	if err != nil {
		t.Skipf("no test database available at %s (set SONGBOOKS_TEST_DATABASE_URL to run these tests): %v", connString, err)
	}
	t.Cleanup(st.Close)

	if _, err := st.db.Exec(ctx, `TRUNCATE people, bands, communities RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("resetting test database: %v", err)
	}
	return st
}
