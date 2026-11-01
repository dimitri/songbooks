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
	"app/internal/store"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "address to listen on")
	dbURL := flag.String("db-url", envOr("DATABASE_URL", "postgres://localhost:5432/songbooks_dev?sslmode=disable"), "Postgres connection string (or set DATABASE_URL)")
	v2Root := flag.String("v2-root", envOr("SONGBOOKS_V2_ROOT", ""), "path to the v2/ Typst pipeline (lib/, render.typ, render-book.typ) — required for PDF export; export is disabled if unset")
	importPath := flag.String("import", "", "one-time import: load every book (directory with a songs/ subdir) under this path into the database, then exit")
	setPasswordUser := flag.String("set-password", "", "one-time admin bootstrap: read a password for this username from stdin, hash it, store it, then exit")
	grantAdminUser := flag.String("grant-admin", "", "one-time bootstrap: grant this username site-wide admin (is_admin), then exit — the only way to create the first admin, since there's no self-serve path")
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

	handler := httpapi.NewServer(st, *v2Root)

	log.Printf("songbooks serving %s on http://%s", *dbURL, *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
