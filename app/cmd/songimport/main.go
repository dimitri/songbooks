// songimport restores a file-based export written by cmd/songexport
// back into a Postgres database — disaster recovery without needing the
// original pg_dump, or a way to inspect/hand-edit a backup before
// reinstalling it (every .song file is plain text; users.json/
// members.json/variants.json/songbooks.json are plain JSON).
//
// Meant to run against a FRESH, already-migrated database (store.New
// runs pending migrations on connect, which is exactly what's wanted
// here, unlike cmd/migratescores' deliberate avoidance of that).
//
// Usage:
//
//	songimport --db "$DATABASE_URL" --in ./songbooks-export [--apply]
//
// Without --apply, only prints what it would create (dry run).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"app/internal/groupfmt"
	"app/internal/setlist"
	"app/internal/store"
)

type user struct {
	ID              int64           `json:"id"`
	Username        string          `json:"username"`
	DisplayName     string          `json:"displayName"`
	Email           string          `json:"email,omitempty"`
	IsAdmin         bool            `json:"isAdmin,omitempty"`
	IsShell         bool            `json:"isShell,omitempty"`
	Instruments     []string        `json:"instruments,omitempty"`
	Nickname        string          `json:"nickname,omitempty"`
	PictureURL      string          `json:"pictureUrl,omitempty"`
	Phone           string          `json:"phone,omitempty"`
	ShareEmail      bool            `json:"shareEmail,omitempty"`
	SharePhone      bool            `json:"sharePhone,omitempty"`
	GravatarOptOut  bool            `json:"gravatarOptOut,omitempty"`
	OnboardedAt     string          `json:"onboardedAt,omitempty"`
	ClickTrackPrefs json.RawMessage `json:"clickTrackPrefs,omitempty"`
	CreatedAt       string          `json:"createdAt"`
}

type songbookMembership struct {
	OwnerKind    string `json:"ownerKind"`
	OwnerName    string `json:"ownerName"`
	SongbookName string `json:"songbookName"`
}

type variantRecord struct {
	ID                 int64                `json:"id"`
	File               string               `json:"file"`
	Title              string               `json:"title"`
	Artist             string               `json:"artist"`
	Visibility         string               `json:"visibility"`
	OwnerKind          string               `json:"ownerKind"`
	OwnerName          string               `json:"ownerName,omitempty"`
	CreatedByUserID    int64                `json:"createdByUserId,omitempty"`
	ForkedFromID       int64                `json:"forkedFromId,omitempty"`
	SongbookMembership []songbookMembership `json:"songbookMemberships,omitempty"`
	Audio              []audioRecord        `json:"audio,omitempty"`
	GPFile             *gpFileRecord        `json:"gpFile,omitempty"`
}

type audioRecord struct {
	Category        string        `json:"category"`
	File            string        `json:"file"`
	ContentType     string        `json:"contentType"`
	SHA256          string        `json:"sha256"`
	SourceURL       string        `json:"sourceUrl,omitempty"`
	CreatedByUserID int64         `json:"createdByUserId,omitempty"`
	SyncPoints      []syncPoint   `json:"syncPoints,omitempty"`
	Markers         []audioMarker `json:"markers,omitempty"`
}

type syncPoint struct {
	BarIndex    int     `json:"barIndex"`
	TimeSeconds float64 `json:"timeSeconds"`
}

type audioMarker struct {
	SectionIndex int     `json:"sectionIndex"`
	TimeSeconds  float64 `json:"timeSeconds"`
}

type gpFileRecord struct {
	File            string `json:"file"`
	Filename        string `json:"filename"`
	ContentType     string `json:"contentType"`
	SHA256          string `json:"sha256"`
	CreatedByUserID int64  `json:"createdByUserId,omitempty"`
}

type setlistGigRecord struct {
	Date  string `json:"date,omitempty"`
	Venue string `json:"venue,omitempty"`
	Notes string `json:"notes,omitempty"`
}

type setlistRecord struct {
	Title     string             `json:"title"`
	Notes     string             `json:"notes,omitempty"`
	Scope     string             `json:"scope"`
	OwnerName string             `json:"ownerName"`
	File      string             `json:"file"`
	Gigs      []setlistGigRecord `json:"gigs,omitempty"`
}

type songGroup struct {
	Label      string  `json:"Label"`
	VariantIDs []int64 `json:"VariantIDs"`
}

type songbookRecord struct {
	OwnerKind string      `json:"ownerKind"`
	OwnerName string      `json:"ownerName"`
	Name      string      `json:"name"`
	IsDefault bool        `json:"isDefault"`
	SongOrder []songGroup `json:"songOrder,omitempty"`
}

type ownerKey struct{ kind, name string }
type songbookKey struct {
	owner ownerKey
	name  string
}

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string")
	inDir := flag.String("in", "", "export directory (as written by songexport)")
	apply := flag.Bool("apply", false, "actually write; without this flag, only prints what would happen")
	flag.Parse()
	if *dbURL == "" || *inDir == "" {
		log.Fatal("usage: songimport --db <url> --in <dir> [--apply]")
	}

	var users []user
	readJSON(filepath.Join(*inDir, "users.json"), &users)
	var variants []variantRecord
	readJSON(filepath.Join(*inDir, "variants.json"), &variants)
	var songbookRecs []songbookRecord
	readJSON(filepath.Join(*inDir, "songbooks.json"), &songbookRecs)
	var setlistRecs []setlistRecord
	readJSON(filepath.Join(*inDir, "setlists.json"), &setlistRecs)

	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	run := &runner{st: st, inDir: *inDir, apply: *apply, newUserID: map[int64]int64{}, usernameByOldID: map[int64]string{},
		personIDByUsername: map[string]int64{},
		ownerID:            map[ownerKey]int64{}, groupCreator: map[ownerKey]int64{}, songbookID: map[songbookKey]int64{}, newVariantID: map[int64]int64{}}

	run.importUsers(ctx, users)
	run.importGroups(ctx, variants) // bands/communities, their members
	run.importSongbooks(ctx, songbookRecs)
	run.importSongs(ctx, variants)
	run.relinkForks(ctx, variants)
	run.addSongbookMemberships(ctx, variants)
	run.restoreSongOrder(ctx, songbookRecs)
	run.importSetlists(ctx, setlistRecs)

	fmt.Printf("\n%d song(s), %d setlist(s), %d owner-group(s) processed.\n", len(variants), len(setlistRecs), len(run.ownerID))
	if !*apply {
		fmt.Println("(dry run — pass --apply to actually write)")
	}
}

type runner struct {
	st    *store.Store
	inDir string
	apply bool

	newUserID          map[int64]int64    // OLD user id (from users.json) -> NEW person id in this database
	usernameByOldID    map[int64]string   // for log messages only
	personIDByUsername map[string]int64   // username -> NEW person id — OwnerName for a person-owned song/songbook is a username (slug), not a users.json id
	ownerID            map[ownerKey]int64 // (kind,name) -> band id / community id / person id
	groupCreator       map[ownerKey]int64 // (kind,name) -> a CURRENT member, for a song whose own created_by is unrecorded; 0 if the group has no members at all
	songbookID         map[songbookKey]int64
	newVariantID       map[int64]int64 // old id -> new id
}

func (r *runner) logf(format string, args ...any) { fmt.Printf(format+"\n", args...) }

func (r *runner) importUsers(ctx context.Context, users []user) {
	for _, u := range users {
		r.usernameByOldID[u.ID] = u.Username
		if !u.IsShell {
			// A shell's username is a random "pending-..." placeholder
			// CreateShellPerson generates fresh each time (see its own
			// doc comment) — there's nothing stable to match an existing
			// one by, so a shell is always (re)created new; only a real
			// account is matched against what's already in this database.
			existing, err := r.st.GetPersonByUsername(ctx, u.Username)
			if err == nil {
				r.newUserID[u.ID] = existing.ID
				r.personIDByUsername[u.Username] = existing.ID
				continue
			}
		}
		r.logf("person %s: creating (display name %q, shell=%v)", u.Username, u.DisplayName, u.IsShell)
		if !r.apply {
			r.newUserID[u.ID] = -1 // placeholder so a dry run can still walk the rest of the logic
			r.personIDByUsername[u.Username] = -1
			continue
		}
		var newID int64
		var err error
		if u.IsShell {
			newID, err = r.st.CreateShellPerson(ctx, u.DisplayName)
		} else {
			newID, err = r.st.CreatePerson(ctx, u.Username, u.DisplayName, u.Email)
		}
		if err != nil {
			log.Fatalf("creating person %s: %v", u.Username, err)
		}
		if u.IsAdmin {
			if err := r.st.SetAdmin(ctx, newID, true); err != nil {
				log.Fatalf("setting admin for %s: %v", u.Username, err)
			}
		}
		if len(u.Instruments) > 0 {
			if err := r.st.SetInstruments(ctx, newID, u.Instruments); err != nil {
				log.Fatalf("setting instruments for %s: %v", u.Username, err)
			}
		}
		if err := r.st.SetPublicProfile(ctx, newID, u.Nickname, u.PictureURL, u.Phone, u.ShareEmail, u.SharePhone, u.GravatarOptOut); err != nil {
			log.Fatalf("setting public profile for %s: %v", u.Username, err)
		}
		createdAt, err := time.Parse(time.RFC3339, u.CreatedAt)
		if err != nil {
			log.Fatalf("parsing createdAt for %s: %v", u.Username, err)
		}
		var onboardedAt *time.Time
		if u.OnboardedAt != "" {
			t, err := time.Parse(time.RFC3339, u.OnboardedAt)
			if err != nil {
				log.Fatalf("parsing onboardedAt for %s: %v", u.Username, err)
			}
			onboardedAt = &t
		}
		if err := r.st.RestorePersonTimestamps(ctx, newID, createdAt, onboardedAt, u.ClickTrackPrefs); err != nil {
			log.Fatalf("restoring timestamps for %s: %v", u.Username, err)
		}
		r.newUserID[u.ID] = newID
		r.personIDByUsername[u.Username] = newID
	}
}

// importGroups creates every band/community referenced in variants.json
// (by reading each owner directory's members.txt), its roster, and its
// songbooks — everything importSongs needs before it can create a
// single song.
func (r *runner) importGroups(ctx context.Context, variants []variantRecord) {
	seen := map[ownerKey]bool{}
	for _, v := range variants {
		for _, m := range v.SongbookMembership {
			k := ownerKey{m.OwnerKind, m.OwnerName}
			if seen[k] || k.kind == "catalog" || k.kind == "person" {
				continue
			}
			seen[k] = true
			r.importGroup(ctx, k)
		}
	}
}

func (r *runner) importGroup(ctx context.Context, k ownerKey) {
	path := filepath.Join(r.inDir, "songbooks", k.name, "members.json")
	mf, err := groupfmt.Read(path)
	if err != nil {
		log.Fatalf("reading %s: %v", path, err)
	}
	sort.SliceStable(mf.Members, func(i, j int) bool { return mf.Members[i].JoinedAt < mf.Members[j].JoinedAt })
	name := mf.Name

	if len(mf.Members) == 0 {
		// Real data, not a malformed export: a legacy band/community can
		// have zero band_members/community_members rows (confirmed live
		// — "Mercy"). Nothing to pick a creator from, so create it bare.
		r.logf("%s %q: creating with no members (none recorded)", k.kind, name)
		if !r.apply {
			r.ownerID[k] = -1
			return
		}
		var groupID int64
		switch k.kind {
		case "band":
			groupID, err = r.st.CreateBandNoMembers(ctx, name)
		case "community":
			groupID, err = r.st.CreateCommunityNoMembers(ctx, name)
		default:
			log.Fatalf("%s: unknown group kind %q", path, k.kind)
		}
		if err != nil {
			log.Fatalf("creating %s %q: %v", k.kind, name, err)
		}
		r.ownerID[k] = groupID
		return
	}

	creatorOldID := mf.Members[0].UserID
	creatorID, ok := r.newUserID[creatorOldID]
	if !ok {
		log.Fatalf("%s: creator (user id %d) not found in users.json", path, creatorOldID)
	}

	r.logf("%s %q: creating with %d member(s), creator=%s", k.kind, name, len(mf.Members), r.usernameByOldID[creatorOldID])
	if !r.apply {
		r.ownerID[k] = -1 // placeholder so importSongs' dry run doesn't also fail to resolve it
		return
	}

	var groupID int64
	switch k.kind {
	case "band":
		groupID, err = r.st.CreateBand(ctx, name, creatorID)
	case "community":
		groupID, err = r.st.CreateCommunity(ctx, name, creatorID)
	default:
		log.Fatalf("%s: unknown group kind %q", path, k.kind)
	}
	if err != nil {
		log.Fatalf("creating %s %q: %v", k.kind, name, err)
	}
	r.ownerID[k] = groupID
	r.groupCreator[k] = creatorID

	if k.kind == "community" {
		builtin := map[string]bool{"staff": true, "member": true, "guest": true}
		for _, role := range mf.Roles {
			if builtin[role.Name] {
				continue // seeded automatically by CreateCommunity, with the same fixed privilege set
			}
			if _, err := r.st.CreateCommunityRole(ctx, groupID, role.Name, role.Privileges); err != nil {
				log.Fatalf("creating role %q in community %q: %v", role.Name, name, err)
			}
		}
	}

	for _, m := range mf.Members[1:] { // [0] is the creator, already a member
		personID, ok := r.newUserID[m.UserID]
		if !ok {
			log.Fatalf("%s: member (user id %d) not found in users.json", path, m.UserID)
		}
		var invitedBy int64
		if m.InvitedBy != 0 {
			invitedBy = r.newUserID[m.InvitedBy]
		}
		var addErr error
		switch k.kind {
		case "band":
			_, addErr = r.st.AddBandMember(ctx, groupID, personID, m.RoleOrStatus, invitedBy)
		case "community":
			_, addErr = r.st.AddCommunityMember(ctx, groupID, personID, m.RoleOrStatus, invitedBy)
		}
		if addErr != nil {
			log.Fatalf("adding %s to %s %q: %v", r.usernameByOldID[m.UserID], k.kind, name, addErr)
		}
		if m.LeftAt != "" {
			if err := r.st.SetMemberLeftAt(ctx, k.kind, groupID, personID, m.LeftAt); err != nil {
				log.Fatalf("marking %s as departed from %s %q: %v", r.usernameByOldID[m.UserID], k.kind, name, err)
			}
		}
	}

	// This group's own songbook(s): the default one came free with
	// CreateBand/CreateCommunity above; anything else (a second
	// songbook, or the default one needing a different name than the
	// generic one CreateBand/CreateCommunity picked) is reconciled once
	// songbooks.json is walked, in importSongs below — not here, since
	// doing it here would need a second read of songbooks.json this
	// function doesn't have.
}

func (r *runner) resolveOwnerID(ctx context.Context, kind, name string) int64 {
	if kind == "person" {
		id, ok := r.personIDByUsername[name]
		if !ok {
			log.Fatalf("person %q not found", name)
		}
		return id
	}
	k := ownerKey{kind, name}
	if id, ok := r.ownerID[k]; ok {
		return id
	}
	log.Fatalf("%s %q was never created (importGroups ran before importSongs?)", kind, name)
	return 0
}

// resolveSongbook finds or creates the songbook named sbName under
// owner (kind, ownerName) — CreateBand/CreateCommunity/a person's first
// CreateVariant call all lazily create a "default" songbook of their
// own under a fixed generic name, so this only actually creates a new
// row when sbName doesn't match anything already there.
// resolveSongbook finds or creates songbookName under owner (kind,
// ownerName). isDefault only matters the one time this actually CREATES
// the songbook (a cache hit, or a name match via FindSongbookByName,
// keeps whatever is_default that row already has) — pass the value
// recorded in songbooks.json whenever the caller has it (importSongbooks
// below, which runs first and populates the cache for everyone else).
func (r *runner) resolveSongbook(ctx context.Context, kind, ownerName, sbName string, isDefault bool) int64 {
	ownerID := r.resolveOwnerID(ctx, kind, ownerName)
	key := songbookKey{ownerKey{kind, ownerName}, sbName}
	if id, ok := r.songbookID[key]; ok {
		return id
	}
	if !r.apply {
		return -1
	}
	id, err := r.st.FindSongbookByName(ctx, kind, ownerID, sbName)
	if err != nil {
		id, err = r.st.CreateSongbook(ctx, kind, ownerID, sbName, isDefault)
		if err != nil {
			log.Fatalf("creating songbook %q for %s %q: %v", sbName, kind, ownerName, err)
		}
		r.logf("songbook %q: created for %s %q", sbName, kind, ownerName)
	}
	r.songbookID[key] = id
	return id
}

// importSongbooks creates (or finds) EVERY songbook from songbooks.json
// up front, before any song/membership/order pass touches one — not
// just the ones a song or song_order entry happens to reference. A
// songbook with no songs in it at all (confirmed live: a person who
// has a personal songbook but has never put a song in it) would
// otherwise never get created, since nothing else in this tool ever
// asks resolveSongbook for it. Running this first also means every
// later resolveSongbook call is a cache hit using the real recorded
// is_default, instead of each one risking creating its own duplicate,
// non-default "Songbook"/"My songbook" row the first time it's asked
// for by name (confirmed live for a band whose only default songbook
// had never been created via CreateBand itself — CreateBand doesn't
// create one eagerly, only CreateBandVariant/CreateVariant do, lazily,
// the first time a song actually needs one).
func (r *runner) importSongbooks(ctx context.Context, songbookRecs []songbookRecord) {
	for _, sb := range songbookRecs {
		r.resolveSongbook(ctx, sb.OwnerKind, sb.OwnerName, sb.Name, sb.IsDefault)
	}
}

func (r *runner) importSongs(ctx context.Context, variants []variantRecord) {
	for _, v := range variants {
		body, err := os.ReadFile(filepath.Join(r.inDir, v.File))
		if err != nil {
			log.Fatalf("reading %s: %v", v.File, err)
		}
		r.logf("song %q (%s): creating under %s %q", v.Title, v.File, v.OwnerKind, v.OwnerName)
		if !r.apply {
			continue
		}
		var creatorID int64
		if v.CreatedByUserID != 0 {
			creatorID = r.newUserID[v.CreatedByUserID]
		}
		// CreateBandVariant/CreateCommunityVariant require the creator to
		// be a CURRENT member — a song with no created_by_person_id on
		// file (real data: confirmed live) would otherwise fail that
		// check with person id 0. Fall back to whoever created the group
		// itself; a handful of pre-membership-tracking legacy groups
		// (e.g. "Mercy") have NO members at all, so even that can be 0 —
		// for those, fall back further to a plain catalog song rather
		// than fail the whole import over one unrecoverable attribution.
		ownerKind := v.OwnerKind
		if creatorID == 0 && (ownerKind == "band" || ownerKind == "community") {
			creatorID = r.groupCreator[ownerKey{ownerKind, v.OwnerName}]
			if creatorID == 0 {
				r.logf("  (no member to attribute %q to in %s %q — importing as a catalog song instead)", v.Title, ownerKind, v.OwnerName)
				ownerKind = "catalog"
			}
		}
		var newID int64
		switch ownerKind {
		case "catalog":
			newID, err = r.st.CreateVariant(ctx, store.OwnerCatalog, 0, v.Title, v.Artist, creatorID)
		case "person":
			newID, err = r.st.CreateVariant(ctx, store.OwnerPerson, r.resolveOwnerID(ctx, "person", v.OwnerName), v.Title, v.Artist, creatorID)
		case "band":
			newID, err = r.st.CreateBandVariant(ctx, r.resolveOwnerID(ctx, "band", v.OwnerName), v.Title, v.Artist, creatorID)
		case "community":
			newID, err = r.st.CreateCommunityVariant(ctx, r.resolveOwnerID(ctx, "community", v.OwnerName), v.Title, v.Artist, creatorID)
		default:
			log.Fatalf("song %q: unknown owner kind %q", v.Title, v.OwnerKind)
		}
		if err != nil {
			log.Fatalf("creating song %q: %v", v.Title, err)
		}
		_, version, err := r.st.LoadVariantRaw(newID)
		if err != nil {
			log.Fatalf("loading fresh song %d: %v", newID, err)
		}
		if _, err := r.st.SaveVariantRaw(newID, string(body), version, creatorID); err != nil {
			log.Fatalf("writing body for song %q: %v", v.Title, err)
		}
		r.newVariantID[v.ID] = newID

		for _, a := range v.Audio {
			data, err := os.ReadFile(filepath.Join(r.inDir, a.File))
			if err != nil {
				log.Fatalf("reading %s: %v", a.File, err)
			}
			audioCreator := r.newUserID[a.CreatedByUserID]
			audioID, err := r.st.SetSongAudio(ctx, newID, store.AudioCategory(a.Category), data, a.ContentType, a.SHA256, a.SourceURL, audioCreator)
			if err != nil {
				log.Fatalf("writing %s audio for song %q: %v", a.Category, v.Title, err)
			}
			if len(a.SyncPoints) > 0 {
				points := make([]store.GPBarSyncPoint, len(a.SyncPoints))
				for i, sp := range a.SyncPoints {
					points[i] = store.GPBarSyncPoint{BarIndex: sp.BarIndex, TimeSeconds: sp.TimeSeconds}
				}
				if err := r.st.SetGPBarSyncPoints(ctx, audioID, points); err != nil {
					log.Fatalf("writing sync points for song %q: %v", v.Title, err)
				}
			}
			for _, m := range a.Markers {
				if err := r.st.SetAudioMarker(ctx, audioID, m.SectionIndex, m.TimeSeconds); err != nil {
					log.Fatalf("writing audio marker for song %q: %v", v.Title, err)
				}
			}
		}
		if v.GPFile != nil {
			data, err := os.ReadFile(filepath.Join(r.inDir, v.GPFile.File))
			if err != nil {
				log.Fatalf("reading %s: %v", v.GPFile.File, err)
			}
			gpCreator := r.newUserID[v.GPFile.CreatedByUserID]
			if err := r.st.SetSongGPSourceFile(ctx, newID, v.GPFile.Filename, v.GPFile.ContentType, data, v.GPFile.SHA256, gpCreator); err != nil {
				log.Fatalf("writing GP source file for song %q: %v", v.Title, err)
			}
		}
	}
}

func (r *runner) relinkForks(ctx context.Context, variants []variantRecord) {
	for _, v := range variants {
		if v.ForkedFromID == 0 {
			continue
		}
		r.logf("song %q: forked from old id %d", v.Title, v.ForkedFromID)
		if !r.apply {
			continue
		}
		newID, ok1 := r.newVariantID[v.ID]
		forkedNewID, ok2 := r.newVariantID[v.ForkedFromID]
		if !ok1 || !ok2 {
			log.Printf("song %q: can't relink fork (missing new id) — skipped", v.Title)
			continue
		}
		if err := r.st.SetForkedFrom(ctx, newID, forkedNewID); err != nil {
			log.Fatalf("relinking fork for song %q: %v", v.Title, err)
		}
	}
}

func (r *runner) addSongbookMemberships(ctx context.Context, variants []variantRecord) {
	for _, v := range variants {
		newID, ok := r.newVariantID[v.ID]
		if !r.apply {
			continue
		}
		if !ok {
			continue
		}
		for _, m := range v.SongbookMembership {
			sbID := r.resolveSongbook(ctx, m.OwnerKind, m.OwnerName, m.SongbookName, false)
			if err := r.st.AddToSongbook(ctx, sbID, newID, 0); err != nil {
				log.Fatalf("adding song %q to songbook %q: %v", v.Title, m.SongbookName, err)
			}
		}
	}
}

func (r *runner) restoreSongOrder(ctx context.Context, songbookRecs []songbookRecord) {
	for _, sb := range songbookRecs {
		if len(sb.SongOrder) == 0 {
			continue
		}
		r.logf("songbook %q (%s %q): restoring song_order (%d group(s))", sb.Name, sb.OwnerKind, sb.OwnerName, len(sb.SongOrder))
		if !r.apply {
			continue
		}
		sbID := r.resolveSongbook(ctx, sb.OwnerKind, sb.OwnerName, sb.Name, sb.IsDefault)
		var groups []store.SongGroup
		for _, g := range sb.SongOrder {
			var ids []int64
			for _, oldID := range g.VariantIDs {
				if newID, ok := r.newVariantID[oldID]; ok {
					ids = append(ids, newID)
				}
			}
			groups = append(groups, store.SongGroup{Label: g.Label, VariantIDs: ids})
		}
		if err := r.st.SaveOrder(ctx, sbID, groups); err != nil {
			log.Fatalf("saving song_order for songbook %q: %v", sb.Name, err)
		}
	}
}

// importSetlists restores every setlist — a distinct artifact from a
// songbook (a specific, ordered list for a gig), so this runs
// independently of the song/songbook passes above, after importGroups/
// importUsers so an owner (person/band/community) always already
// exists to attach to.
func (r *runner) importSetlists(ctx context.Context, setlistRecs []setlistRecord) {
	for _, sl := range setlistRecs {
		body, err := os.ReadFile(filepath.Join(r.inDir, sl.File))
		if err != nil {
			log.Fatalf("reading %s: %v", sl.File, err)
		}
		r.logf("setlist %q (%s %q): creating", sl.Title, sl.Scope, sl.OwnerName)
		if !r.apply {
			continue
		}
		var ownerPersonID, ownerBandID, ownerCommunityID *int64
		var actorID int64
		switch sl.Scope {
		case "person":
			id := r.resolveOwnerID(ctx, "person", sl.OwnerName)
			ownerPersonID, actorID = &id, id
		case "band":
			id := r.resolveOwnerID(ctx, "band", sl.OwnerName)
			ownerBandID = &id
			actorID = r.groupCreator[ownerKey{"band", sl.OwnerName}]
		case "community":
			id := r.resolveOwnerID(ctx, "community", sl.OwnerName)
			ownerCommunityID = &id
			actorID = r.groupCreator[ownerKey{"community", sl.OwnerName}]
		default:
			log.Fatalf("setlist %q: unknown scope %q", sl.Title, sl.Scope)
		}
		newID, err := r.st.CreateSetlist(ctx, sl.Scope, sl.Title, sl.Notes, ownerPersonID, ownerBandID, ownerCommunityID, actorID)
		if err != nil {
			log.Fatalf("creating setlist %q: %v", sl.Title, err)
		}
		meta, err := r.st.GetSetlistMeta(ctx, newID)
		if err != nil {
			log.Fatalf("loading fresh setlist %d: %v", newID, err)
		}
		if _, err := r.st.SaveSetlistByID(ctx, newID, setlist.Parse(string(body)), sl.Title, sl.Notes, strconv.Itoa(meta.Version), actorID); err != nil {
			log.Fatalf("writing body for setlist %q: %v", sl.Title, err)
		}
		for _, g := range sl.Gigs {
			var date *time.Time
			if g.Date != "" {
				t, err := time.Parse("2006-01-02", g.Date)
				if err != nil {
					log.Fatalf("parsing gig date %q for setlist %q: %v", g.Date, sl.Title, err)
				}
				date = &t
			}
			if _, err := r.st.AddSetlistGig(ctx, newID, date, g.Venue, g.Notes); err != nil {
				log.Fatalf("adding gig to setlist %q: %v", sl.Title, err)
			}
		}
	}
}

func readJSON(path string, v any) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("reading %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		log.Fatalf("parsing %s: %v", path, err)
	}
}
