package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// sbdemo rewrites a whole database. It runs only on a copy: a database
// on this machine (localhost, 127.0.0.1) or staging's compose database
// (host "db"), and only when told, with --confirm, the database and host
// it is about to change, as "songbooks@db".
var allowedHosts = map[string]bool{"localhost": true, "127.0.0.1": true, "db": true}

// Production runs on the same machine as staging, its compose database
// also called db: what tells them apart inside the app's container is the
// hostname passkeys are bound to (SONGBOOKS_WEBAUTHN_RP_ID, set on both).
var productionHosts = map[string]bool{
	"app.songbooks.studio": true, "songbooks.studio": true, "songbooks.fr": true, "app.songbooks.fr": true,
}

// checkDeployment refuses inside production's container, and on a
// compose database (db) whose deployment doesn't say it isn't production.
func checkDeployment(host string, getenv func(string) string) error {
	rp := strings.ToLower(strings.TrimSpace(getenv("SONGBOOKS_WEBAUTHN_RP_ID")))
	if productionHosts[rp] {
		return fmt.Errorf("refusing: this is production (SONGBOOKS_WEBAUTHN_RP_ID=%s)", rp)
	}
	// A deployment that says it's a demo copy (SONGBOOKS_DEMO_DATA=1: the
	// local stack, staging) may be rewritten on its compose database.
	if host == "db" && rp == "" && getenv("SONGBOOKS_DEMO_DATA") == "1" {
		return nil
	}
	if host == "db" && rp == "" {
		return fmt.Errorf("refusing: on host db, SONGBOOKS_WEBAUTHN_RP_ID must name the deployment (staging's) to tell it from production")
	}
	return nil
}

// target is the database and host a connection string names.
func target(dbURL string) (database, host string, err error) {
	if strings.TrimSpace(dbURL) == "" {
		return "", "", fmt.Errorf("no database: pass --db or set DATABASE_URL")
	}
	cfg, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return "", "", fmt.Errorf("reading the connection string: %w", err)
	}
	return cfg.Database, cfg.Host, nil
}

// checkConfirm refuses unless dbURL's host is one sbdemo may change and
// confirm says exactly which database and host that is.
func checkConfirm(dbURL, confirm string) (database, host string, err error) {
	database, host, err = target(dbURL)
	if err != nil {
		return "", "", err
	}
	if !allowedHosts[host] {
		return "", "", fmt.Errorf("refusing: host %q is not a local or staging database (only localhost, 127.0.0.1 and db are)", host)
	}
	if database == "" {
		return "", "", fmt.Errorf("refusing: the connection string names no database")
	}
	want := database + "@" + host
	if confirm == "" {
		return "", "", fmt.Errorf("refusing: this rewrites every name in %s; pass --confirm %s to go on", want, want)
	}
	if confirm != want {
		return "", "", fmt.Errorf("refusing: --confirm %s does not match the database, %s", confirm, want)
	}
	return database, host, nil
}

// checkConnected makes sure the server reached is the database confirmed
// (a connection string's defaults, a service file or PG* variables could
// otherwise point elsewhere).
func checkConnected(ctx context.Context, conn interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, database string) error {
	var got string
	if err := conn.QueryRow(ctx, `SELECT current_database()`).Scan(&got); err != nil {
		return err
	}
	if got != database {
		return fmt.Errorf("refusing: connected to database %q, not %q", got, database)
	}
	return nil
}
