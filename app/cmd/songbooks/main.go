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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	// The image has no zoneinfo: TZ (the deploy files set Europe/Paris)
	// is read from the copy embedded here. Event and rehearsal times are
	// shown in that zone.
	_ "time/tzdata"

	"golang.org/x/crypto/bcrypt"

	"app/internal/covers"
	"app/internal/httpapi"
	"app/internal/mailer"
	"app/internal/reftitle"
	"app/internal/songimport"
	"app/internal/store"
	"app/internal/ytaudio"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "address to listen on")
	dbURL := flag.String("db-url", envOr("DATABASE_URL", "postgres://localhost:5432/songbooks_dev?sslmode=disable"), "Postgres connection string (or set DATABASE_URL)")
	setPasswordUser := flag.String("set-password", "", "one-time bootstrap: read a password for this person (an email or a numeric id) from stdin, hash it, store it, then exit")
	grantAdminUser := flag.String("grant-admin", "", "one-time bootstrap: grant this person (an email or a numeric id) site-wide admin (is_admin), then exit — the only way to create the first admin, since there's no self-serve path")
	setEmailUser := flag.String("set-email", "", "one-time bootstrap: set (or change) an account's email, the account's identity and what it logs in with — pass the person here (their numeric id, or their current email) and the new email as the one remaining positional argument, e.g. songbooks --set-email 3 me@example.com")
	createPersonalInviteUser := flag.String("create-personal-invite", "", "one-time bootstrap: mint a fresh /invite/{token} link for an existing shell account (one created via the band/community roster's quick-add, is_shell=true, not yet logged in), or an account nobody ever got into (no email, password, passkey or session: it becomes a shell), and print it, then exit — the admin-panel equivalent of the roster page's 'Get invite link' button, for an account that has no admin band/community member around to click it. Pass the shell account's numeric id (it has no email yet).")
	smtpURL := flag.String("smtp-url", "", "real SMTP endpoint for outbound mail — NOT YET IMPLEMENTED; if set, this is logged and the server falls back to the console/outbox mailer. Swapping in a real mailer.Mailer implementation here is the contained follow-up change this flag is a placeholder for.")
	devAutoLoginUser := flag.String("dev-auto-login", envOr("SONGBOOKS_DEV_AUTO_LOGIN", ""), "DEV/TESTING ONLY, DEFAULT OFF: if set to a person (an email or a numeric id), every single HTTP request is automatically authenticated as that person — /login is skipped entirely, no cookie, no session. This completely disables authentication for anyone who can reach the server. NEVER set this (as a flag or as the SONGBOOKS_DEV_AUTO_LOGIN env var) on a production or otherwise publicly-reachable deployment. Leave it unset (the default) everywhere except your own local dev loop.")
	webauthnRPID := flag.String("webauthn-rp-id", envOr("SONGBOOKS_WEBAUTHN_RP_ID", ""), "passkey (WebAuthn) Relying Party ID — the bare hostname passkeys are bound to (e.g. songbooks.tapoueh.org). MUST be set explicitly in production: a passkey only ever works against the exact RPID it was registered under, and defaulting this is a dev-only convenience (see below). Left unset (the default), the RPID/origin are instead derived per-request from that request's own Host header — fine for bouncing between localhost:8080 and a LAN IP while developing, but wrong for a real deployment, which should pin one fixed hostname here instead of trusting whatever Host a request arrived with.")
	soundfontPath := flag.String("soundfont", envOr("SONGBOOKS_SOUNDFONT", ""), "the SoundFont 2 bank scores play with, served cut down to each score's presets at /soundfonts/subset/ (docs/soundfont.md); default: GeneralUser GS where the image puts it (/usr/local/share/songbooks) or deploy/fetch-soundfont.sh does (the cache directory). None, scores play with alphaTab's Sonivox")
	cacheDir := flag.String("cache-dir", envOr("SONGBOOKS_CACHE_DIR", defaultCacheDir()), "a directory for files the app makes and can make again (the soundfont subsets)")
	soundfontCacheMB := flag.Int64("soundfont-cache-mb", 256, "the most the soundfont subsets may take in the cache directory, in MB (the least recently used go first)")
	adminFileDir := flag.String("admin-file-dir", envOr("SONGBOOKS_ADMIN_FILE_DIR", ""), "DEFAULT OFF: a directory to serve flat (no subdirectories) at GET /admin/files/{name}, admin-auth-gated — a small staging area for getting a large local file (a bulk-import source, say) onto this app's own origin, so a browser session already signed in here can fetch it directly instead of hitting mixed-content/CORS blocks against a separate host. Leave unset outside of a deliberate one-off bulk-import operation.")
	flag.Parse()

	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	// Instruments in the profile (migration 0101): what the app already
	// knew people play, collected onto their profiles. Adds only what's
	// missing, so it's a no-op once done.
	if n, err := st.BackfillPersonInstruments(ctx); err != nil {
		log.Printf("collecting instruments onto profiles: %v", err)
	} else if n > 0 {
		log.Printf("collected %d instruments onto profiles", n)
	}
	defer st.Close()

	if *setPasswordUser != "" {
		p, err := st.FindPerson(ctx, *setPasswordUser)
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
		p, err := st.FindPerson(ctx, *grantAdminUser)
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
		p, err := st.FindPerson(ctx, *setEmailUser)
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
		p, err := st.FindPerson(ctx, *createPersonalInviteUser)
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
		p, err := st.FindPerson(ctx, *devAutoLoginUser)
		if err != nil {
			log.Fatalf("--dev-auto-login %q: %v", *devAutoLoginUser, err)
		}
		devAutoLogin = p
		log.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		log.Printf("!!! DEV AUTO-LOGIN IS ACTIVE: every request is auto-authenticated as %-20q !!!", p.DisplayName)
		log.Printf("!!! ALL AUTHENTICATION IS DISABLED. /login is bypassed for every visitor.      !!!")
		log.Printf("!!! THIS MUST NEVER RUN ON A PRODUCTION OR PUBLICLY-REACHABLE DEPLOYMENT.      !!!")
		log.Printf("!!! If you are seeing this banner on songbooks.tapoueh.org (or any other live  !!!")
		log.Printf("!!! deployment), stop the server NOW: unset --dev-auto-login /                 !!!")
		log.Printf("!!! SONGBOOKS_DEV_AUTO_LOGIN and restart.                                       !!!")
		log.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	}

	// The email is the account (migration 0135): one with a password and
	// no email can't log in any more. Said at every start, like the admin
	// page does, until each has one.
	if locked, err := st.ListPasswordNoEmail(ctx); err != nil {
		log.Printf("listing the accounts without an email: %v", err)
	} else if len(locked) > 0 {
		for _, p := range locked {
			log.Printf("account %d (%q) has a password but no email: it can't log in — songbooks --set-email %d <email>", p.ID, p.DisplayName, p.ID)
		}
	}

	// No login email reaches anyone (the console mailer): an account with
	// an email but no password nor passkey (event registration made them
	// until a password was required) can't log back in once its session
	// ends. Said at every start, like the admin page does.
	if !mailer.Delivers(m) {
		if locked, err := st.ListEmailNoPassword(ctx); err != nil {
			log.Printf("listing the accounts without a password: %v", err)
		} else {
			for _, p := range locked {
				log.Printf("account %d (%q) has an email but no password nor passkey, and no login email is sent: it can't log back in — an admin's login link, then a password on its profile, or songbooks --set-password %d", p.ID, p.DisplayName, p.ID)
			}
		}
	}

	handler := httpapi.NewServer(st, m, devAutoLogin, *webauthnRPID, *adminFileDir)
	httpapi.Covers = covers.NewFinder()
	httpapi.RefTitles = reftitle.New()
	// Quick add's lyrics: the singer's chart, as the editor's Import
	// fetches it (Ultimate Guitar's lyrics, no chords; iReal Pro's grids).
	httpapi.QuickDraft = func(ctx context.Context, st *store.Store, title, artist string) (string, error) {
		d, err := songimport.VocalDraft(ctx, st, title, artist)
		if err != nil {
			return "", err
		}
		return d.Text, nil
	}
	// A band's proposal (docs/band-votes.md): the new song's chart as the
	// quick add fetches it, with where it came from, and its recording.
	httpapi.ProposalDraft = func(ctx context.Context, st *store.Store, title, artist string) (*httpapi.ProposalDraftResult, error) {
		d, err := songimport.VocalDraft(ctx, st, title, artist)
		if err != nil {
			return nil, err
		}
		return &httpapi.ProposalDraftResult{Text: d.Text, URL: d.URL, IReal: d.IReal, Notes: d.Notes}, nil
	}
	httpapi.ProposalAudio = ytaudio.Download
	// A demo copy (the local stack, staging; never production) gets the
	// admin page's demo data panel (docs/demo-data.md).
	httpapi.DemoData = os.Getenv("SONGBOOKS_DEMO_DATA") == "1"
	httpapi.DemoDBURL = *dbURL
	// The soundfont scores play with (docs/soundfont.md): GeneralUser GS
	// where the image or deploy/fetch-soundfont.sh put it.
	if *soundfontPath == "" {
		*soundfontPath = "/usr/local/share/songbooks/GeneralUser-GS.sf2"
		if local := filepath.Join(*cacheDir, "GeneralUser-GS.sf2"); fileExists(local) {
			*soundfontPath = local
		}
	}
	httpapi.UseSoundFont(*soundfontPath, filepath.Join(*cacheDir, "soundfont-subsets"), *soundfontCacheMB<<20)
	// Live lessons (docs/live-lessons.md): Galène's address and the key
	// its lessons group checks our join tokens with (galene/setup.sh
	// writes it in deploy/.env); no key, no online lessons to join.
	if k := os.Getenv("SONGBOOKS_GALENE_KEY"); k != "" {
		key, err := httpapi.ParseGaleneKey(k)
		if err != nil {
			log.Fatalf("SONGBOOKS_GALENE_KEY: %v", err)
		}
		httpapi.Galene = httpapi.GaleneConfig{
			URL: strings.TrimRight(envOr("SONGBOOKS_GALENE_URL", "https://live.songbooks.studio"), "/"),
			Key: key,
			KID: envOr("SONGBOOKS_GALENE_KID", "songbooks-1"),
		}
		log.Printf("live lessons on %s", httpapi.Galene.URL)
	}

	// Each recording's melody range, from its vocals part, in the
	// background (docs/coaching.md).
	go httpapi.RunMelodyRanges(context.Background(), st)
	// And its notes, for the song page's Melody view (docs/melody-sheet.md).
	go httpapi.RunMelodyNotes(context.Background(), st)
	log.Printf("songbooks serving %s on http://%s", redactURL(*dbURL), *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}

// defaultCacheDir is the user's cache directory's songbooks folder, or
// the system's temporary one's.
func defaultCacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "songbooks")
	}
	return filepath.Join(os.TempDir(), "songbooks-cache")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// redactURL is a connection string with its password masked, for logs.
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return s
	}
	if _, ok := u.User.Password(); ok {
		u.User = url.UserPassword(u.User.Username(), "xxxxx")
	}
	return u.String()
}
