// Command songbooks serves the songbooks web app: a single static binary
// (all templates/assets are go:embed'd) backed by Postgres — songbooks,
// songs and setlists all live in the database, migrated automatically on
// startup.
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
	"time"

	"golang.org/x/crypto/bcrypt"

	"app/internal/httpapi"
	"app/internal/mailer"
	"app/internal/store"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "address to listen on")
	dbURL := flag.String("db-url", envOr("DATABASE_URL", "postgres://localhost:5432/songbooks_dev?sslmode=disable"), "Postgres connection string (or set DATABASE_URL)")
	setPasswordUser := flag.String("set-password", "", "one-time bootstrap: read a password for this username from stdin, hash it, store it, then exit — secondary/legacy auth path now that /login is magic-link-only")
	grantAdminUser := flag.String("grant-admin", "", "one-time bootstrap: grant this username site-wide admin (is_admin), then exit — the only way to create the first admin, since there's no self-serve path")
	setEmailUser := flag.String("set-email", "", "one-time bootstrap: set a pre-existing account's email so it can use magic-link login — pass the username here and the email as the one remaining positional argument, e.g. songbooks --set-email tapoueh me@example.com")
	createPersonalInviteUser := flag.String("create-personal-invite", "", "one-time bootstrap: mint a fresh /invite/{token} link for an existing shell account (one created via the band/community roster's quick-add, is_shell=true, not yet logged in) and print it, then exit — the admin-panel equivalent of the roster page's 'Get invite link' button, for an account that has no admin band/community member around to click it. Pass the shell account's username.")
	createBandInviteID := flag.Int64("create-band-invite", 0, "one-time bootstrap: mint a fresh single-use /invite/{token} band invite link (see handleCreateBandInvite) for this band ID and print it, then exit — the admin-panel equivalent of the band members page's 'Add to band' action with no username typed in. Status defaults to \"core\"; pass \"guest\" as the one positional argument to override.")
	smtpURL := flag.String("smtp-url", "", "real SMTP endpoint for outbound mail — NOT YET IMPLEMENTED; if set, this is logged and the server falls back to the console/outbox mailer. Swapping in a real mailer.Mailer implementation here is the contained follow-up change this flag is a placeholder for.")
	devAutoLoginUser := flag.String("dev-auto-login", envOr("SONGBOOKS_DEV_AUTO_LOGIN", ""), "DEV/TESTING ONLY, DEFAULT OFF: if set to a username, every single HTTP request is automatically authenticated as that person — /login is skipped entirely, no cookie, no session. This completely disables authentication for anyone who can reach the server. NEVER set this (as a flag or as the SONGBOOKS_DEV_AUTO_LOGIN env var) on a production or otherwise publicly-reachable deployment. Leave it unset (the default) everywhere except your own local dev loop.")
	webauthnRPID := flag.String("webauthn-rp-id", envOr("SONGBOOKS_WEBAUTHN_RP_ID", ""), "passkey (WebAuthn) Relying Party ID — the bare hostname passkeys are bound to (e.g. songbooks.tapoueh.org). MUST be set explicitly in production: a passkey only ever works against the exact RPID it was registered under, and defaulting this is a dev-only convenience (see below). Left unset (the default), the RPID/origin are instead derived per-request from that request's own Host header — fine for bouncing between localhost:8080 and a LAN IP while developing, but wrong for a real deployment, which should pin one fixed hostname here instead of trusting whatever Host a request arrived with.")
	flag.Parse()

	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer st.Close()

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

	if *createPersonalInviteUser != "" {
		p, err := st.GetPersonByUsername(ctx, *createPersonalInviteUser)
		if err != nil {
			log.Fatalf("looking up %q: %v", *createPersonalInviteUser, err)
		}
		token, err := st.CreatePersonalInvite(ctx, p.ID, 0, 30*24*time.Hour)
		if err != nil {
			log.Fatalf("creating personal invite for %q: %v", *createPersonalInviteUser, err)
		}
		fmt.Printf("/invite/%s\n", token)
		return
	}

	if *createBandInviteID != 0 {
		status := "core"
		args := flag.Args()
		if len(args) > 1 {
			log.Fatalf("--create-band-invite takes at most one positional argument (\"core\" or \"guest\"), got %d", len(args))
		}
		if len(args) == 1 {
			status = args[0]
		}
		token, err := st.CreateBandInvite(ctx, *createBandInviteID, status, 0, 7*24*time.Hour)
		if err != nil {
			log.Fatalf("creating band invite for band %d: %v", *createBandInviteID, err)
		}
		fmt.Printf("/invite/%s\n", token)
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

	handler := httpapi.NewServer(st, m, devAutoLogin, *webauthnRPID)

	log.Printf("songbooks serving %s on http://%s", *dbURL, *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
