package store

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrate applies every embedded migration not yet recorded in
// schema_migrations, in filename order (0001_, 0002_, ...). Idempotent:
// safe to call on every startup.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version int PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		// The app and the stems worker (cmd/stemsd) start side by side:
		// one applies a migration, the other waits, then finds it applied.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7240531)`); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if applied {
			tx.Rollback(ctx)
			continue
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("applying migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func migrationVersion(filename string) (int, error) {
	stem, _, _ := strings.Cut(filename, "_")
	n, err := strconv.Atoi(stem)
	if err != nil {
		return 0, fmt.Errorf("migration filename %q doesn't start with a numeric version", filename)
	}
	return n, nil
}

// OpenCurrent connects to a database whose schema is already this
// binary's, without migrating it: for an administrator's command that must
// not change a database's schema as a side effect (cmd/ownership's dry
// run, on staging, say). A database with a migration this binary has that
// it hasn't, or none at all, is refused.
func OpenCurrent(ctx context.Context, connString string) (*Store, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		pool.Close()
		return nil, err
	}
	for _, e := range entries {
		version, err := migrationVersion(e.Name())
		if err != nil {
			pool.Close()
			return nil, err
		}
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			pool.Close()
			return nil, fmt.Errorf("reading schema_migrations: %w", err)
		}
		if !applied {
			pool.Close()
			return nil, fmt.Errorf("the database lacks migration %s: run this with the app's own version", e.Name())
		}
	}
	return &Store{db: pool}, nil
}
