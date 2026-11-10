// songimport restores a file-based export written by cmd/songexport
// back into a Postgres database — disaster recovery without needing the
// original pg_dump, or a way to inspect/hand-edit a backup before
// reinstalling it (every .song file and members.txt is plain, readable
// text).
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

	"app/internal/membersfmt"
	"app/internal/store"
)

type person struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email,omitempty"`
	IsAdmin     bool     `json:"isAdmin,omitempty"`
	Instruments []string `json:"instruments,omitempty"`
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
	CreatedByUsername  string               `json:"createdByUsername,omitempty"`
	ForkedFromID       int64                `json:"forkedFromId,omitempty"`
	SongbookMembership []songbookMembership `json:"songbookMemberships,omitempty"`
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

	var people []person
	readJSON(filepath.Join(*inDir, "people.json"), &people)
	var variants []variantRecord
	readJSON(filepath.Join(*inDir, "variants.json"), &variants)
	var songbookRecs []songbookRecord
	readJSON(filepath.Join(*inDir, "songbooks.json"), &songbookRecs)

	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	run := &runner{st: st, inDir: *inDir, apply: *apply, personIDByUsername: map[string]int64{},
		ownerID: map[ownerKey]int64{}, groupCreator: map[ownerKey]int64{}, songbookID: map[songbookKey]int64{}, newVariantID: map[int64]int64{}}

	run.importPeople(ctx, people)
	run.importGroups(ctx, variants) // bands/communities, their members
	run.importSongbooks(ctx, songbookRecs)
	run.importSongs(ctx, variants)
	run.relinkForks(ctx, variants)
	run.addSongbookMemberships(ctx, variants)
	run.restoreSongOrder(ctx, songbookRecs)

	fmt.Printf("\n%d song(s), %d owner-group(s) processed.\n", len(variants), len(run.ownerID))
	if !*apply {
		fmt.Println("(dry run — pass --apply to actually write)")
	}
}

type runner struct {
	st    *store.Store
	inDir string
	apply bool

	personIDByUsername map[string]int64
	ownerID            map[ownerKey]int64 // (kind,name) -> band id / community id / person id
	groupCreator       map[ownerKey]int64 // (kind,name) -> a CURRENT member, for a song whose own created_by is unrecorded; 0 if the group has no members at all
	songbookID         map[songbookKey]int64
	newVariantID       map[int64]int64 // old id -> new id
}

func (r *runner) logf(format string, args ...any) { fmt.Printf(format+"\n", args...) }

func (r *runner) importPeople(ctx context.Context, people []person) {
	for _, p := range people {
		existing, err := r.st.GetPersonByUsername(ctx, p.Username)
		if err == nil {
			r.personIDByUsername[p.Username] = existing.ID
			continue
		}
		r.logf("person %s: creating (display name %q)", p.Username, p.DisplayName)
		if !r.apply {
			r.personIDByUsername[p.Username] = -1 // placeholder so a dry run can still walk the rest of the logic
			continue
		}
		newID, err := r.st.CreatePerson(ctx, p.Username, p.DisplayName, p.Email)
		if err != nil {
			log.Fatalf("creating person %s: %v", p.Username, err)
		}
		r.personIDByUsername[p.Username] = newID
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
	path := filepath.Join(r.inDir, "songbooks", k.name, "members.txt")
	mf, err := membersfmt.Read(path)
	if err != nil {
		log.Fatalf("reading %s: %v", path, err)
	}
	sort.SliceStable(mf.Members, func(i, j int) bool { return mf.Members[i].JoinedAt < mf.Members[j].JoinedAt })
	name := mf.Header["name"]

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

	creatorUsername := mf.Members[0].Username
	creatorID, ok := r.personIDByUsername[creatorUsername]
	if !ok {
		log.Fatalf("%s: creator %q not found in people.json", path, creatorUsername)
	}

	r.logf("%s %q: creating with %d member(s), creator=%s", k.kind, name, len(mf.Members), creatorUsername)
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
		personID, ok := r.personIDByUsername[m.Username]
		if !ok {
			log.Fatalf("%s: member %q not found in people.json", path, m.Username)
		}
		var invitedBy int64
		if m.InvitedBy != "" {
			invitedBy = r.personIDByUsername[m.InvitedBy]
		}
		var addErr error
		switch k.kind {
		case "band":
			_, addErr = r.st.AddBandMember(ctx, groupID, personID, m.RoleOrStatus, invitedBy)
		case "community":
			_, addErr = r.st.AddCommunityMember(ctx, groupID, personID, m.RoleOrStatus, invitedBy)
		}
		if addErr != nil {
			log.Fatalf("adding %s to %s %q: %v", m.Username, k.kind, name, addErr)
		}
		if m.LeftAt != "" {
			if err := r.st.SetMemberLeftAt(ctx, k.kind, groupID, personID, m.LeftAt); err != nil {
				log.Fatalf("marking %s as departed from %s %q: %v", m.Username, k.kind, name, err)
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
		if v.CreatedByUsername != "" {
			creatorID = r.personIDByUsername[v.CreatedByUsername]
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

func readJSON(path string, v any) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("reading %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		log.Fatalf("parsing %s: %v", path, err)
	}
}
