// Command songbooks serves the songbooks web app: a single static binary
// (all templates/assets are go:embed'd) backed by Postgres — projects,
// songs and setlists all live in the database, migrated automatically on
// startup. Pass --import to one-time load an existing v2/ directory tree
// instead of serving.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"app/internal/httpapi"
	"app/internal/mailer"
	"app/internal/store"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "address to listen on")
	dbURL := flag.String("db-url", envOr("DATABASE_URL", "postgres://localhost:5432/songbooks_dev?sslmode=disable"), "Postgres connection string (or set DATABASE_URL)")
	v2Root := flag.String("v2-root", envOr("SONGBOOKS_V2_ROOT", ""), "path to the v2/ Typst pipeline (lib/, render.typ, render-book.typ) — required for PDF export; export is disabled if unset")
	importPath := flag.String("import", "", "one-time import: load every book (directory with a songs/ subdir) under this path into the database, then exit")
	setPasswordUser := flag.String("set-password", "", "one-time bootstrap: read a password for this username from stdin, hash it, store it, then exit — secondary/legacy auth path now that /login is magic-link-only")
	grantAdminUser := flag.String("grant-admin", "", "one-time bootstrap: grant this username site-wide admin (is_admin), then exit — the only way to create the first admin, since there's no self-serve path")
	setEmailUser := flag.String("set-email", "", "one-time bootstrap: set a pre-existing account's email so it can use magic-link login — pass the username here and the email as the one remaining positional argument, e.g. songbooks --set-email tapoueh me@example.com")
	smtpURL := flag.String("smtp-url", "", "real SMTP endpoint for outbound mail — NOT YET IMPLEMENTED; if set, this is logged and the server falls back to the console/outbox mailer. Swapping in a real mailer.Mailer implementation here is the contained follow-up change this flag is a placeholder for.")
	devAutoLoginUser := flag.String("dev-auto-login", envOr("SONGBOOKS_DEV_AUTO_LOGIN", ""), "DEV/TESTING ONLY, DEFAULT OFF: if set to a username, every single HTTP request is automatically authenticated as that person — /login is skipped entirely, no cookie, no session. This completely disables authentication for anyone who can reach the server. NEVER set this (as a flag or as the SONGBOOKS_DEV_AUTO_LOGIN env var) on a production or otherwise publicly-reachable deployment. Leave it unset (the default) everywhere except your own local dev loop.")
	flag.Parse()

	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer st.Close()

	if *importPath != "" {
		if err := store.ImportTree(ctx, st, *importPath); err != nil {
			log.Fatalf("import failed: %v", err)
		}
		return
	}

	if *setPasswordUser != "" {
		p, err := st.GetPersonByUsername(ctx, *setPasswordUser)
		if err != nil {
			log.Fatalf("looking up %q: %v", *setPasswordUser, err)
		}
		fmt.Printf("password for %s: ", *setPasswordUser)
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			log.Fatal("no password read from stdin")
		}
		password := strings.TrimSpace(scanner.Text())
		if password == "" {
			log.Fatal("empty password")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("hashing password: %v", err)
		}
		if err := st.SetPassword(ctx, p.ID, string(hash)); err != nil {
			log.Fatalf("saving password: %v", err)
		}
		fmt.Printf("password set for %s\n", *setPasswordUser)
		return
	}

	if *grantAdminUser != "" {
		p, err := st.GetPersonByUsername(ctx, *grantAdminUser)
		if err != nil {
			log.Fatalf("looking up %q: %v", *grantAdminUser, err)
		}
		if err := st.SetAdmin(ctx, p.ID, true); err != nil {
			log.Fatalf("granting admin: %v", err)
		}
		fmt.Printf("%s is now an admin\n", *grantAdminUser)
		return
	}

	if *setEmailUser != "" {
		args := flag.Args()
		if len(args) != 1 {
			log.Fatalf("--set-email %q needs exactly one positional argument (the email address), got %d", *setEmailUser, len(args))
		}
		email := strings.ToLower(strings.TrimSpace(args[0]))
		if email == "" {
			log.Fatal("empty email")
		}
		p, err := st.GetPersonByUsername(ctx, *setEmailUser)
		if err != nil {
			log.Fatalf("looking up %q: %v", *setEmailUser, err)
		}
		if err := st.SetEmail(ctx, p.ID, email); err != nil {
			log.Fatalf("saving email: %v", err)
		}
		fmt.Printf("email set for %s: %s\n", *setEmailUser, email)
		return
	}

	// SMTP isn't wired up yet (out of scope for this phase — no mail
	// server exists in this environment); the console/outbox mailer
	// below is the deliverable. Swapping in a real mailer.Mailer
	// implementation when one is needed is meant to be a contained
	// change: construct it here instead of mailer.NewConsoleMailer, gated
	// on *smtpURL, and nothing else in the codebase (store, httpapi)
	// should need to change.
	if *smtpURL != "" {
		log.Printf("--smtp-url %q given, but real SMTP is not yet implemented — falling back to the console mailer", *smtpURL)
	}
	m := mailer.NewConsoleMailer(st)

	var devAutoLogin *store.Person
	if *devAutoLoginUser != "" {
		p, err := st.GetPersonByUsername(ctx, *devAutoLoginUser)
		if err != nil {
			log.Fatalf("--dev-auto-login %q: %v", *devAutoLoginUser, err)
		}
		devAutoLogin = p
		log.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		log.Printf("!!! DEV AUTO-LOGIN IS ACTIVE: every request is auto-authenticated as %-20q !!!", p.Username)
		log.Printf("!!! ALL AUTHENTICATION IS DISABLED. /login is bypassed for every visitor.      !!!")
		log.Printf("!!! THIS MUST NEVER RUN ON A PRODUCTION OR PUBLICLY-REACHABLE DEPLOYMENT.      !!!")
		log.Printf("!!! If you are seeing this banner on songbooks.tapoueh.org (or any other live  !!!")
		log.Printf("!!! deployment), stop the server NOW: unset --dev-auto-login /                 !!!")
		log.Printf("!!! SONGBOOKS_DEV_AUTO_LOGIN and restart.                                       !!!")
		log.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	}

	handler := httpapi.NewServer(st, *v2Root, m, devAutoLogin)

	log.Printf("songbooks serving %s on http://%s", *dbURL, *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
