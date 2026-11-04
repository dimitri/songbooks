package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/chordsym"
	"app/internal/song"
	"app/internal/store"
	"app/internal/webassets"
)

type bookStats struct {
	Name      string
	SongCount int
	SetCount  int // groups in order.txt (or 1 for the alphabetical fallback)
	HasOrder  bool
	Setlists  int
}

// attentionItem is one row of the home page's "needs your attention"
// feed — band proposals the viewer can vote on, plus their own unread
// notifications (proposal decisions, school submission decisions, ...).
// Deliberately NOT a full activity log: band edits already have their
// own home for this ("your bands" card's digest line below), and
// group-invite slot claims don't fire a notification today (see
// store.CreateNotification's callers), so that category is left out
// rather than faked.
type attentionItem struct {
	Kind     string // "votes" or "notifications" — drives the filter chips
	Title    string
	Subtitle string
	When     string
	Link     string
}

type bandCardView struct {
	ID        int64
	Name      string
	Members   int
	Digest    string
	OpenVotes int

	// Phase 32: direct links from the bands page to this band's own
	// song list/set lists — SetlistCount/FirstSetlistID let the template
	// link straight to "the" setlist when there's exactly one, or to the
	// grouped setlists index otherwise; NextGig* is this band's own
	// soonest upcoming gig, if any.
	SetlistCount   int
	FirstSetlistID int64

	HasNextGig     bool
	NextGigDate    time.Time
	NextGigVenue   string
	NextGigSetlist int64
}

// homeGreeting returns a time-of-day greeting line — purely cosmetic,
// computed server-side so it's correct on first paint (no client flash).
func homeGreeting() string {
	switch h := time.Now().Hour(); {
	case h < 5:
		return "Good night"
	case h < 12:
		return "Good morning"
	case h < 18:
		return "Good afternoon"
	default:
		return "Good evening"
	}
}

// timeAgo renders a short relative-time label ("2h ago", "3d ago") for
// the attention feed — no need for anything fancier than day/hour/minute
// granularity on a feed that's capped at a handful of recent items.
func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// handleIndex is just the project directory: name + a few stats per book,
// click through to /b/{book} for the actual set list. Keeping it light
// means it stays fast and legible even with many books/projects.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)

	// Resume step for the group-invite "log in to claim this slot"
	// sub-flow (see join.go's pendingJoinCookie doc comment): a visitor
	// who wasn't logged in picked a slot, got redirected through
	// /login, and has just landed back here, now authenticated. One-shot
	// — the cookie is cleared regardless of outcome, so a failed claim
	// (slot already taken, invite closed meanwhile) just falls through
	// to the ordinary dashboard rather than retrying forever.
	if cookie, err := r.Cookie(pendingJoinCookie); err == nil {
		http.SetCookie(w, &http.Cookie{Name: pendingJoinCookie, Value: "", Path: "/", MaxAge: -1})
		if token, slotParam, ok := strings.Cut(cookie.Value, ":"); ok {
			if dest, err := s.claimGroupInviteForPerson(r.Context(), token, slotParam, person.ID, person.DisplayName); err == nil {
				http.Redirect(w, r, dest, http.StatusFound)
				return
			}
		}
	}

	views, err := s.buildBookStats(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bands, err := s.store.ListBandsForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bandCards, attention, err := s.buildBandCards(r.Context(), person.ID, bands)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	notifs, err := s.store.ListNotifications(r.Context(), person.ID, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, n := range notifs {
		attention = append(attention, attentionItem{
			Kind: "notifications", Title: n.Body, When: timeAgo(n.CreatedAt), Link: n.Link,
		})
	}
	sort.SliceStable(attention, func(i, j int) bool { return i < j }) // already newest-first per source; a true merge-sort by time isn't worth it for a handful of items

	gigs, err := s.store.ListUpcomingGigsForPerson(r.Context(), person.ID, 1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var nextGig *store.UpcomingGig
	if len(gigs) > 0 {
		nextGig = &gigs[0]
	}

	s.render(w, "index", map[string]any{
		"Books": views, "Me": person, "Greeting": homeGreeting(),
		"Bands": bandCards, "Attention": attention, "NextGig": nextGig,
	})
}

// buildBookStats computes the per-project "book" stats (song/set/setlist
// counts) for every project personID can see, alphabetical by name.
// Shared by handleIndex (the home page's "Your songbooks" teaser) and
// handleCatalogPage (Phase 32's "by songbook" view) so both pages show
// identical project stats rather than two implementations that can drift.
func (s *Server) buildBookStats(ctx context.Context, personID int64) ([]bookStats, error) {
	projects, err := s.store.ListProjectsForPerson(ctx, personID)
	if err != nil {
		return nil, err
	}
	var views []bookStats
	for _, pr := range projects {
		b := pr.Slug
		groups, err := s.store.ListSongsOrdered(b)
		if err != nil {
			return nil, err
		}
		songCount := 0
		for _, g := range groups {
			songCount += len(g.Stems)
		}
		setlists, _ := s.store.ListSetlists(b)
		views = append(views, bookStats{
			Name: b, SongCount: songCount, SetCount: len(groups),
			HasOrder: s.store.HasOrderFile(b), Setlists: len(setlists),
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })
	return views, nil
}

// buildBandCards computes the per-band "card" data (member count, a
// digest of changes since personID's last visit, open-proposal count)
// plus any open-proposal rows for the attention feed, for a given list
// of bands personID currently belongs to. Shared by handleIndex (the
// home page) and handleBandsPage (the bands listing, Phase 27) so both
// pages show the exact same activity summary per band rather than two
// versions that can drift.
func (s *Server) buildBandCards(ctx context.Context, personID int64, bands []store.Band) ([]bandCardView, []attentionItem, error) {
	var bandCards []bandCardView
	var attention []attentionItem

	// One query for every band's soonest upcoming gig, rather than one
	// per band — ListUpcomingGigsForPerson is already ordered soonest
	// first, so the first row per band id is that band's next gig.
	gigs, err := s.store.ListUpcomingGigsForPerson(ctx, personID, 200)
	if err != nil {
		return nil, nil, err
	}
	nextGigByBand := map[int64]store.UpcomingGig{}
	for _, g := range gigs {
		if g.BandID == nil {
			continue
		}
		if _, ok := nextGigByBand[*g.BandID]; !ok {
			nextGigByBand[*g.BandID] = g
		}
	}

	for _, b := range bands {
		members, err := s.store.ListBandMembers(ctx, b.ID, false)
		if err != nil {
			return nil, nil, err
		}
		lastSeen, err := s.store.GetBandLastSeen(ctx, b.ID, personID)
		if err != nil {
			return nil, nil, err
		}
		changes, err := s.store.ListBandChanges(ctx, b.ID, lastSeen)
		if err != nil {
			return nil, nil, err
		}
		digest := fmt.Sprintf("%d change%s since your last visit", len(changes), plural(len(changes)))
		if lastSeen == nil {
			digest = "you haven't visited yet"
		}

		proposals, err := s.store.ListProposals(ctx, b.ID, store.ProposalFilter{Status: "open"})
		if err != nil {
			return nil, nil, err
		}

		setlists, err := s.store.ListSetlistsForBand(ctx, b.ID)
		if err != nil {
			return nil, nil, err
		}
		var firstSetlistID int64
		if len(setlists) > 0 {
			firstSetlistID = setlists[0].ID
		}

		card := bandCardView{
			ID: b.ID, Name: b.Name, Members: len(members), Digest: digest, OpenVotes: len(proposals),
			SetlistCount: len(setlists), FirstSetlistID: firstSetlistID,
		}
		if g, ok := nextGigByBand[b.ID]; ok {
			card.HasNextGig = true
			card.NextGigDate = g.Date
			card.NextGigVenue = g.Venue
			card.NextGigSetlist = g.SetlistID
		}
		bandCards = append(bandCards, card)
		for _, p := range proposals {
			title := fmt.Sprintf("Vote: add %q to %s's repertoire", p.TargetTitle, b.Name)
			if p.Kind == "new_reference" {
				title = fmt.Sprintf("Vote: new reference for %q", p.TargetTitle)
			}
			attention = append(attention, attentionItem{
				Kind: "votes", Title: title,
				Subtitle: fmt.Sprintf("Proposed by %s · %d yes, %d veto so far", p.ProposerUsername, p.YesVotes, p.VetoVotes),
				When:     timeAgo(p.CreatedAt),
				Link:     fmt.Sprintf("/bands/%d/proposals/%d", b.ID, p.ID),
			})
		}
	}
	sort.Slice(bandCards, func(i, j int) bool { return bandCards[i].Name < bandCards[j].Name })
	return bandCards, attention, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// handleProject is one book's set list — the numbered Set 1/Set 2/... view
// (from order.txt, or a flat alphabetical fallback) plus the "+ song"
// control; this is what clicking a project on the home page opens.
// projectSongRow enriches a bare stem with the title/tempo/key a musician
// actually orients by — same reasoning, and same per-song LoadSong lookup,
// as the gig view's gigRow: a list of raw filenames ("its-too-late",
// "worried-life-blues") reads as a directory listing, not a songbook.
type projectSongRow struct {
	Stem, Title, Tempo, Key string
}

type projectGroup struct {
	Label string
	Songs []projectSongRow
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	book := r.PathValue("book")
	groups, err := s.store.ListSongsOrdered(book)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var out []projectGroup
	for _, g := range groups {
		pg := projectGroup{Label: g.Label}
		for _, stem := range g.Stems {
			row := projectSongRow{Stem: stem, Title: stem}
			if sg, _, err := s.store.LoadSong(book, stem); err == nil {
				row.Title = orDefault(sg.Get("title"), stem)
				row.Tempo = sg.Get("tempo")
				row.Key = sg.Get("key")
			}
			pg.Songs = append(pg.Songs, row)
		}
		out = append(out, pg)
	}
	meta := s.store.LoadBookMeta(book)
	s.render(w, "project", map[string]any{
		"Me": person, "Book": book, "Groups": out, "Playlist": meta.Playlist,
	})
}

// viewerBeat is one quarter-beat slot in a bar's rhythm-slash notation (the
// Real Book / LilyPond rhythmic-staff convention this chart imitates):
// every beat gets its own slash mark on the staff line, but a chord symbol
// is only written above the beat where it actually starts — a held chord's
// later beats are blank slashes (ties), never a repeated or dashed symbol.
type viewerBeat struct {
	Text  string // chord/rest symbol if this beat is an attack, "" if it's a tie
	IsTie bool
	// Span is how many beats (this one plus any immediately-following tied
	// beats it absorbs) this attack actually lasts — 0 for a tie beat
	// itself. Jazz mode collapses tied beats out of the flex row entirely
	// (see the CSS), so without this every surviving chord got an equal
	// flex:1 share regardless of how long it's actually held — a bar
	// written "Gm . . Cm" (Gm for 3 beats, Cm for 1) rendered as a 50/50
	// split instead of 75/25, losing exactly the "chord change on beat 4"
	// clarity the beat is meant to show.
	Span int
}

type viewerMeasure struct {
	Index     int
	GlobalBar int // 0-based bar index across the whole song, for the clock -> DOM mapping
	Beats     []viewerBeat
	IsRepeat  bool // this bar is "%" or "%%" — rendered as a simile sign, not beat slashes
	// RepeatCount is 1 for a one-bar simile ("%") and 2 for a two-bar
	// simile ("%%", standard Real Book shorthand for "same as the previous
	// two bars") — only meaningful when IsRepeat. Matches the Typst
	// pipeline's own simile(..., count) (v2/lib/staff.typ): a "%%" token is
	// still a single written measure (same grid-cell width, same bar
	// count/GlobalBar accounting as any other bar) with a wider glyph and
	// a small "2" printed above it, not two measures merged into one.
	RepeatCount int
	// IsRest marks a bar whose only token is "R" — a full bar of silence,
	// engraved as the standard whole-bar rest (a filled block hanging
	// below the staff's 2nd line, v2/lib/staff.typ's own rest()), not a
	// literal "R" chord symbol with ordinary slashes under it.
	IsRest     bool
	Volta      string
	VoltaFirst bool // this bar starts a volta run — only the first bar of a multi-bar ending shows the "1." label
	Meter      string
	Open       bool
	Close      bool
	// BracketRepeatCount is set ONLY on the CLOSE bar of a bracket whose
	// source stated its own repeat count ("|:4 ...", carried on the OPEN
	// measure — see song.Measure.RepeatCount's doc comment) — 0 for
	// every other bar, including a plain count-less "|: ..." bracket
	// (the implicit, unlabeled "x2" every repeat sign already means with
	// nothing printed). Printed on the close side, not the open side
	// where the count is actually stated in source: real engraving
	// (checked against LilyPond) places a repeat count at the END of the
	// repeated span (buildSections carries it across the span to get
	// there). Deliberately NOT named RepeatCount — that name is already
	// taken on this struct by the simile ("%%") glyph's own, unrelated
	// count.
	BracketRepeatCount int
	// Clef is set only on the very first bar of the whole song's first
	// grid — real engraving shows the time signature and tempo mark once,
	// not on every system.
	Clef                    bool
	Tempo, TimeNum, TimeDen string
	// Placeholder marks a bar that isn't real grid data — it's filler
	// generated for a bare "Solo" section so the section visually occupies
	// the same amount of page space as the section it solos over, instead
	// of collapsing to a one-line note. Rendered as empty, dimmed slashes.
	Placeholder bool
	// MapAnnotate/MapMark/MapLabel are set ONLY by buildMapLines, on
	// whichever measure is the first bar of its section in Song Map's
	// flattened continuous score — every other mode's per-section
	// rendering leaves these zero-value, a no-op in "chart-bar" (see
	// play.html). This is how a section's name rides along as a small
	// floating annotation on its own first bar instead of a separate
	// element that would force a row break between sections.
	MapAnnotate       bool
	MapMark, MapLabel string
}

// expandBeats maps a bar's raw tokens onto beatsPerBar rhythm-slash slots.
// Two authoring styles both appear in real .song files, and both need to
// land on the correct beat:
//   - "Gm" alone (one token) for a whole bar: spread across all
//     beatsPerBar slots, chord shown on beat 1 only, the rest ties. This is
//     also how a 2-token bar like "Gm D" (half/half) is spread.
//   - "Gm . D ." (explicit dots, already one token per beat): already
//     beatsPerBar tokens long, so each maps 1:1 to its slot, no spreading.
//
// A token count that divides beatsPerBar evenly is spread; anything else
// (an odd subdivision, or more tokens than beats) falls back to one slot
// per raw token with no beat-alignment claim — still correct content, just
// not fraction-of-a-beat precise, which the grammar doesn't encode anyway.
func expandBeats(tokens []string, beatsPerBar int) []viewerBeat {
	if len(tokens) == 1 && (tokens[0] == "%" || tokens[0] == "%%") {
		return []viewerBeat{{Text: tokens[0], Span: 1}}
	}
	hasDot := false
	for _, t := range tokens {
		if t == "." {
			hasDot = true
			break
		}
	}
	if !hasDot && beatsPerBar > 0 && len(tokens) > 0 && len(tokens) < beatsPerBar && beatsPerBar%len(tokens) == 0 {
		span := beatsPerBar / len(tokens)
		out := make([]viewerBeat, 0, beatsPerBar)
		for _, t := range tokens {
			out = append(out, viewerBeat{Text: song.TokenChord(t)})
			for i := 1; i < span; i++ {
				out = append(out, viewerBeat{IsTie: true})
			}
		}
		return computeSpans(out)
	}
	out := make([]viewerBeat, 0, len(tokens))
	for _, t := range tokens {
		if t == "." {
			out = append(out, viewerBeat{IsTie: true})
			continue
		}
		out = append(out, viewerBeat{Text: song.TokenChord(t)})
	}
	return computeSpans(out)
}

// computeSpans assigns each attack beat its Span (itself plus however many
// tied beats immediately follow it) — see viewerBeat.Span's doc comment.
func computeSpans(beats []viewerBeat) []viewerBeat {
	for i := range beats {
		if beats[i].IsTie {
			continue
		}
		span := 1
		for j := i + 1; j < len(beats) && beats[j].IsTie; j++ {
			span++
		}
		beats[i].Span = span
	}
	return beats
}

type viewerLyricSeg struct {
	HasChord bool
	Chord    string
	Text     string
}

type viewerLyricLine struct {
	Index    int
	Segments []viewerLyricSeg
}

type viewerSection struct {
	Index    int
	Label    string
	Repeat   bool
	Measures []viewerMeasure // flat, used for the "first bar of this section" lookup
	// Times mirrors song.Section.Times (0/1 = a single pass; > 1 means
	// this section — its own grid, or, when bare, whatever grid it
	// inherits below — is actually played that many times through). Used
	// by this section's own BarCount/RepeatSegmentBars math below AND, for
	// a bare section, re-applied as a multiplier on top of an inherited
	// section's numbers in annotateStructure — a bare section's own
	// "times:" is about how many times IT repeats, independent of
	// whatever the section it borrows content from does.
	Times int
	Lines    []viewerLine    // grouped into chart "systems" (rows), per section.BarsPerLine
	Lyrics   []viewerLyricLine
	// Notes are this section occurrence's own free-text performance
	// directions ("GTR + claviers only") — see song.Section.Notes's doc
	// comment for the full design (per-occurrence, not per-bar-row).
	Notes []string
	// InferredNote annotates a bare "Solo" section (no grid of its own —
	// the normal way these are written: "solo over the changes") with how
	// long it runs, guessed from the nearest earlier section that actually
	// has a grid — exactly what a musician reading the chart would assume.
	InferredNote string
	// IsReference marks a bare section (no grid, no lyrics) that repeats an
	// earlier *defining* section of the same label — a jazz-chart "DS/DC"
	// situation in miniature: the content is already written out once
	// above, so this occurrence is a reference back to it, not a second
	// copy. Jazz mode renders a reference as a one-line pointer instead of
	// either re-printing the whole chart (loop-unrolled) or silently
	// vanishing (losing the form entirely).
	IsReference bool
	// GridDuplicateOf is set when this section wrote its OWN grid (unlike
	// IsReference's bare sections) that is token-for-token identical to an
	// earlier section's — confirmed real .song data does this (e.g. a
	// Verse that states the exact same "grid:" line as the Intro above
	// it), not a guess from matching labels. Holds the earlier section's
	// display label ("Intro", "Verse 1"); empty when this section's grid
	// is original. A duplicate's own Lines are left empty (see
	// buildSections) so the identical chart isn't printed twice — this
	// section's own Lyrics, which may be genuinely new words, still render
	// normally alongside a compact repeat mark in their place.
	GridDuplicateOf string
	// Compact marks a section that should render as a slim structural chip
	// even though it has no note/reference text to show — a section with no
	// grid of its own whose chords simply carry on from an earlier one.
	// Earlier this printed "chords continue from X" on every such row;
	// given real setlists this is most sections in a typical pop song, that
	// read as noise rather than information once you'd seen it a few
	// times — the chip alone (keeping the section visible in the jazz
	// chart, which is the part that actually matters) is enough.
	Compact bool
	// IsSolo marks ANY section labeled "Solo..." — set regardless of
	// whether it's a bare/inferred placeholder or a fully defined section
	// with its own real grid/lyrics (e.g. a sung ad-lib over a guitar
	// solo). Unlike InferredNote (which only ever applies to the bare
	// case), this drives a visual-only "this is a Solo" marker
	// (chip + multi-rest bar, same component bare/compact sections use)
	// shown ALONGSIDE whatever real content the section has, not instead
	// of it — a defined Solo section with its own lyrics otherwise
	// rendered identically to a plain Verse, which read as the vocalist
	// singing straight through the solo instead of it being its own,
	// structurally distinct section.
	IsSolo bool
	// BarCount is this section's bar count for the song-map/structure view
	// (the "Couplet 8" style roadmap): the section's own written bars, or —
	// for a Solo's inferred placeholder bars, a reference, or a compact
	// "chords continue" section — the bar count of whatever earlier
	// section it borrows its length from. 0 means genuinely unknown.
	BarCount int
	// StartBar is this section's position in the song's REAL performed bar
	// sequence — assigned in the numbering pass below, which is the whole
	// fix for "the metronome doesn't respect the structure": a bare/
	// reference section (no .Lines, no rendered .bar elements at all)
	// still takes BarCount real bars to actually play, so it still has to
	// consume that many numbers here, or every section after it drifts out
	// of sync with the clock by exactly the bars that got skipped.
	StartBar int
	// RepeatSegmentBars is nonzero when this section's whole grid is one
	// "|: ... :|" bracket (see simpleRepeatSpan) — the written length of
	// ONE pass. A compact/reference display then shows two multi-rest
	// bars of this length side by side ("8" "8") instead of one combined
	// bar claiming "16", which reads as the section's real length but
	// hides that it's a repeated 8-bar phrase, not a genuinely-16-bar one.
	RepeatSegmentBars int
	// MultiRestSegments/MultiRestPer drive the multi-measure-rest glyph(s)
	// in Song/Lyrics mode (see the "multi-rest-row" template block):
	// MultiRestSegments separate glyphs, each standing for MultiRestPer
	// bars — LilyPond's own convention for "N bars, nothing new to show"
	// (a bold bar with tall end-posts, bounded by thin measure barlines,
	// the bar count printed above it). RepeatSegmentBars==0 (a plain
	// single pass, not a repeat) is 1 glyph of BarCount bars;
	// RepeatSegmentBars>0 (this section is really M repeats of an N-bar
	// phrase — a literal "|: ... :|" grid, or a bare section whose
	// "times:" multiplies an inherited length) is M glyphs of N bars
	// each, not one combined glyph claiming M*N — exactly how LilyPond
	// prints two consecutive R1*12 rests as two separate "12" rests, not
	// one "24". Computed once, after annotateStructure has finalized
	// every section's BarCount/RepeatSegmentBars (including inherited
	// ones), so every rendering mode reads the same two numbers instead
	// of re-deriving them (and risking drifting out of sync with the
	// chip's own BarCount total).
	MultiRestSegments int
	MultiRestPer      int
	// LabelNumber distinguishes "which Verse is this" for a musician
	// calling out a part by name ("let's start from Verse 2") — 0 when
	// this label occurs only once in the song (no "Intro 1" for a song
	// with a single Intro), else its 1-based occurrence order among
	// sections sharing the same label.
	LabelNumber int
	// Mark is this section's iReal-Pro-style rehearsal letter ("A", "B",
	// "C", ...) — assigned once per distinct Label, in order of first
	// appearance, by assignRehearsalMarks. It's a compact badge alongside
	// the section's own (richer) descriptive name, not a replacement for
	// it: iReal's single-letter convention plus this app's real section
	// names, rather than picking one over the other.
	Mark string
	// JazzSegno/JazzDSAlCoda/JazzCoda/JazzHidden drive jazz-mode-ONLY
	// Segno/Dal Segno al Coda/Coda navigation signs (see
	// detectSegnoRepeat) — modeled on LilyPond's own \repeat segno
	// semantics (checked against the LilyPond Notation Reference's
	// "Segno repeat structure" section, not approximated from the iReal
	// Pro PDF images alone): Segno marks the jump-BACK target, D.S. al
	// Coda marks the jump-FROM point (where a later, genuinely
	// multi-section repeated run gets collapsed into one marker instead
	// of being re-rendered section by section), and Coda marks where
	// play resumes afterward. This is deliberately a DIFFERENT, bigger
	// structure than \repeat volta — the bracket-repeat convention this
	// app already uses (GridDuplicateOf/IsReference) for a single
	// repeated section, which stays exactly as it is. Every other view
	// mode ignores these fields and renders each section's own content
	// normally; only jazz mode's CSS/template branch (data-render-
	// mode="jazz") acts on them.
	JazzSegno    bool
	JazzDSAlCoda bool // this section's own content is replaced by a "D.S. al Coda" marker
	JazzCoda     bool // a Coda (𝄌, U+1D10C) mark renders before this section
	JazzHidden   bool // part of the collapsed repeated run (after its first section) — fully hidden in jazz mode
}

// viewerLine is one chart "system" (row) within a section's grid: its bars,
// plus Capacity — the row's ACTUAL bar count (len(Measures)), echoed onto
// --cap so the row's bars divide its line's own full width evenly. Every
// row — a full one or a short trailing one — stretches to the same shared
// line width as every other row and as the lyric text beneath it (the
// "staff width" the chart and the lyrics both share); a short row's bars
// are simply wider per bar than a full row's, not the row itself narrower.
type viewerLine struct {
	Measures []viewerMeasure
	Capacity int
}

// groupMeasures chunks a section's measures into display rows — "systems",
// in chart terms — per its BarsPerLine: a single value is a uniform
// measures-per-row target (the FILE format's own default, when "bars:" is
// absent, is 4 — see song.Section.BarsPerLine's doc comment — but this
// view's own display default is 8, matching iReal Pro's own continuous,
// denser chart; narrower screens still shrink a row's target further at
// render time via reflowStaffLines in song.js); several values
// ("bars: 4,5") are explicit, sequential row targets, with the last value
// repeating for any remaining measures. A row's target only decides WHERE
// it breaks — its displayed Capacity is always its own actual bar count
// (set below), so a short row (fewer measures than the target, e.g. a
// 5-bar section against the 8-bar default) still stretches to the line's
// full width instead of rendering visibly narrower than other rows.
func groupMeasures(measures []viewerMeasure, barsPerLine []int) []viewerLine {
	if len(measures) == 0 {
		return nil
	}
	sizes := barsPerLine
	if len(sizes) == 0 {
		sizes = []int{8}
	}
	var lines []viewerLine
	i := 0
	row := 0
	for i < len(measures) {
		n := sizes[row]
		if row < len(sizes)-1 {
			row++
		}
		if n <= 0 {
			n = 1
		}
		end := i + n
		if end > len(measures) {
			end = len(measures)
		}
		lines = append(lines, viewerLine{Measures: measures[i:end], Capacity: end - i})
		i = end
	}
	return lines
}

// mapCell is one rendered unit of Song Map's continuous score: either a
// real measure (with MapAnnotate/MapMark/MapLabel set on it when it's the
// first bar of its section — see viewerMeasure's doc comment) or a single
// compact "recall" glyph standing in for an entire recalled section's
// worth of bars. See buildMapLines.
type mapCell struct {
	IsRecall bool
	Measure  viewerMeasure // zero value when IsRecall

	// Recall-only — a recall cell IS its section's first (only) bar, so it
	// carries its own annotation directly rather than via Measure.
	SectionStart bool
	Mark, Label  string
	RecallBars   int
	RecallOf     string // "" unless GridDuplicateOf named a differently-labeled source section
}

type mapLine struct {
	Cells []mapCell
}

// buildMapLines flattens every section's bars into ONE continuous bar
// sequence for Song Map mode. Every other mode (chords+lyrics, jazz,
// lyrics-only) still renders section-by-section, each with its own
// independent row-grouping (groupMeasures/vs.Lines) — a section always
// starts a fresh row there, however short. Song Map instead wants a single
// engraved score where bars from one section flow straight into the next
// section's bars on the same staff-line row, with section names riding
// along as small floating annotations rather than forcing a new row (the
// actual, specific ask: "drop the forced line breaks, embed them within a
// single multi-line score"). A recalled section — a bare repeat/
// reference, a "chords continue" compact section, an inferred-note Solo,
// or a verbatim grid duplicate under a different label — still collapses
// to one compact multi-rest cell, not its own repeated bars; that's the
// exact same predicate the old per-section view's "is-compact" styling
// used (see play.html's sheet-body template), just evaluated here instead
// of left to CSS.
func buildMapLines(sections []viewerSection) []mapLine {
	var cells []mapCell
	for _, sec := range sections {
		label := sec.Label
		if sec.LabelNumber != 0 {
			label = fmt.Sprintf("%s %d", label, sec.LabelNumber)
		}
		isRecall := sec.GridDuplicateOf != "" || sec.Repeat || sec.IsReference || sec.Compact || sec.InferredNote != ""
		if isRecall {
			if sec.BarCount <= 0 {
				continue // nothing to show at all (an unresolved reference)
			}
			cells = append(cells, mapCell{
				IsRecall: true, SectionStart: true, Mark: sec.Mark, Label: label,
				RecallBars: sec.BarCount, RecallOf: sec.GridDuplicateOf,
			})
			continue
		}
		for mi, m := range sec.Measures {
			if mi == 0 {
				m.MapAnnotate, m.MapMark, m.MapLabel = true, sec.Mark, label
			}
			cells = append(cells, mapCell{Measure: m})
		}
	}
	return groupMapCells(cells, 8)
}

// groupMapCells chunks the flat cell stream into display rows, the same
// 8-cells/line target every mode's own default row uses (groupMeasures) —
// a row can start or end mid-section, nothing about a section boundary
// forces a break. Unlike groupMeasures this doesn't echo a row Capacity
// for CSS to stretch bars against: map-mode bars use a flat typeset width
// (app.css), not a percentage-of-row width, so a short trailing row is
// simply narrower, never stretched.
func groupMapCells(cells []mapCell, perLine int) []mapLine {
	if len(cells) == 0 {
		return nil
	}
	if perLine <= 0 {
		perLine = 8
	}
	var lines []mapLine
	for i := 0; i < len(cells); i += perLine {
		end := i + perLine
		if end > len(cells) {
			end = len(cells)
		}
		lines = append(lines, mapLine{Cells: cells[i:end]})
	}
	return lines
}

// buildSections converts a parsed Song into the view model shared by the
// play page and the prep live-preview fragment — one renderer, so "what you
// preview is what you play."
// realBarCount is how many bars a section actually takes to play through
// once — which is NOT always len(measures):
//
//   - A volta (alternate ending) bracket writes each ending as its own
//     measure ("(1.) Gm | (2.) Eb"), but they share one bar POSITION —
//     only one is ever played on a given pass. Only the first-seen volta
//     label in a run counts; a later, different label immediately
//     following it is an alternate, not a new bar.
//   - A "|: ... :|" repeat bracket is played twice by default, so every
//     bar from the opening to the closing barline (inclusive) counts
//     double — or, when the bracket states its own count ("|:4 ...",
//     see song.Measure.RepeatCount's doc comment), that many times
//     instead of the implicit 2. This is the section's real length, used
//     both for the song-map/structure "N bars" label and for the clock's
//     own timing (StartBar/GlobalBar in buildSections below) — a
//     repeated 8-bar phrase really does take 16 (or N*8) bars of real
//     time to play through, and the metronome needs to wait that long
//     before moving on to the next section, not just 8.
func realBarCount(measures []song.Measure) int {
	n := 0
	firstVoltaLabel := ""
	inRepeat := false
	repeatMult := 1
	for _, m := range measures {
		counts := true
		if m.Volta != "" {
			if firstVoltaLabel == "" {
				firstVoltaLabel = m.Volta
			} else if m.Volta != firstVoltaLabel {
				counts = false
			}
		} else {
			firstVoltaLabel = ""
		}
		if m.Open {
			inRepeat = true
			repeatMult = m.RepeatCount
			if repeatMult <= 0 {
				repeatMult = 2
			}
		}
		mult := 1
		if inRepeat {
			mult = repeatMult
		}
		if counts {
			n += mult
		}
		if m.Close {
			inRepeat = false
			repeatMult = 1
		}
	}
	return n
}

// simpleRepeatSpan reports whether measures is wholly one "|: ... :|"
// bracket (opens on the first bar, closes on the last, exactly once) —
// the common case ("Chorus" is an 8-bar phrase played twice). Returns the
// WRITTEN bar count of one pass through it. A section whose repeat only
// covers PART of its bars (not the whole written grid) returns ok=false —
// the two-segment multi-rest display below only applies to the simple,
// common case, not an interior repeat sitting alongside other content.
func simpleRepeatSpan(measures []song.Measure) (written int, ok bool) {
	if len(measures) == 0 || !measures[0].Open || !measures[len(measures)-1].Close {
		return 0, false
	}
	opens, closes := 0, 0
	for _, m := range measures {
		if m.Open {
			opens++
		}
		if m.Close {
			closes++
		}
	}
	if opens != 1 || closes != 1 {
		return 0, false
	}
	mult := measures[0].RepeatCount
	if mult <= 0 {
		mult = 2
	}
	return realBarCount(measures) / mult, true
}

// beatIR is one beat's normalized, comparable chord content — by pitch
// class (enharmonic-insensitive: F# and Gb compare equal) and decomposed
// quality marker+extension (via chordsym, the same parser the real chart
// rendering uses), not by literal token spelling. "N.C.", "%", and any
// other non-chord token chordsym can't decompose fall back to raw-string
// comparison in Other, since there's nothing more meaningful to normalize.
type beatIR struct {
	rootPC int
	bassPC int
	marker chordsym.Marker
	ext    string
	other  string
}

func normalizeBeatText(text string) beatIR {
	ir := beatIR{rootPC: -1, bassPC: -1}
	if text == "" {
		return ir
	}
	p := chordsym.Split(text)
	if p.Root == "" {
		ir.other = p.Quality
		return ir
	}
	dq := chordsym.DecomposeQuality(p.Quality)
	ir.rootPC, ir.marker, ir.ext = chordsym.PitchClass(p.Root), dq.Marker, dq.Ext
	if p.Bass != "" {
		ir.bassPC = chordsym.PitchClass(p.Bass)
	}
	return ir
}

// measureIR is a measure's musical content, normalized: its beats'
// chords, forward-filled across sustain/tie beats via the same
// expandBeats this file already uses to RENDER a measure — two measures
// that voice the same progression with a different sub-beat split (one
// token stretched to fill the bar vs. the same chord typed at every
// beat, or a differing run of "." sustains) expand to the same per-beat
// sequence here, so they compare equal. Meter is kept alongside as
// musically real (a bar in a different time signature is not "the same
// bar" even with identical chords) — Open/Close/Volta are deliberately
// NOT part of this: they're repeat-barline/ending notation, not musical
// content, and two measures voiced identically shouldn't fail to match
// just because a repeat bracket happens to start or end on one of them.
type measureIR struct {
	beats []beatIR
	meter string
}

func normalizeMeasure(m song.Measure, beatsPerBar int) measureIR {
	expanded := expandBeats(m.Tokens, beatsPerBar)
	beats := make([]beatIR, len(expanded))
	var last beatIR
	for i, b := range expanded {
		if b.IsTie {
			beats[i] = last
			continue
		}
		cur := normalizeBeatText(b.Text)
		beats[i] = cur
		last = cur
	}
	return measureIR{beats: beats, meter: m.Meter}
}

func measureIRsEqual(a, b measureIR) bool {
	if a.meter != b.meter || len(a.beats) != len(b.beats) {
		return false
	}
	for i := range a.beats {
		if a.beats[i] != b.beats[i] {
			return false
		}
	}
	return true
}

// measuresEqual reports whether two sections are the SAME MUSIC — compared
// on a normalized Intermediate Representation (measureIR, via
// normalizeMeasure above), not on literal token/struct equality. A
// previous version required identical token spelling, identical sub-beat
// splitting, and identical Open/Close/Volta repeat-barline flags; this
// drove every "is this section the same as an earlier one" decision in
// the app (Song Map's compact recall chip, Chords+lyrics' "same as X"
// note, Jazz mode's repeat brackets and Segno/D.S. detection below), so
// two sections voiced identically but typed or repeat-bracketed
// differently used to wrongly compare unequal. beatsPerBarA/B are each
// section's OWN beats-per-bar (its "time:" override, or the song's
// global one) — the two sides being compared can legitimately differ.
func measuresEqual(a, b []song.Measure, beatsPerBarA, beatsPerBarB int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !measureIRsEqual(normalizeMeasure(a[i], beatsPerBarA), normalizeMeasure(b[i], beatsPerBarB)) {
			return false
		}
	}
	return true
}

// sectionsEqual reports whether two RAW song sections (not the built
// viewerSection) are the same musical content, for Segno/D.S. detection.
// Same label (the song author's own structural claim — "Verse" recurring
// IS the author saying "this is the Verse again") and, when both wrote a
// grid of their own, semantically equal measures (measuresEqual). A
// section with no grid (an explicit repeat marker, or an empty
// placeholder) is taken at the label's word: two same-labeled sections
// where at least one has no grid to check content against are treated as
// the same section recurring — the same label+emptiness inference this
// file's IsReference/Compact detection already leans on elsewhere.
func sectionsEqual(a, b song.Section, beatsA, beatsB int) bool {
	if a.Label != b.Label {
		return false
	}
	if len(a.Measures) > 0 && len(b.Measures) > 0 {
		return measuresEqual(a.Measures, b.Measures, beatsA, beatsB)
	}
	return true
}

// segnoMatch is a detected Segno/D.S. al Coda structure: the earlier run
// of sections [SegnoAt, SegnoAt+Len) is verbatim-repeated (sectionsEqual,
// pairwise, in order) by the later run [DSAt, DSAt+Len).
type segnoMatch struct {
	SegnoAt, DSAt, Len int
}

// detectSegnoRepeat looks for the real-notation Segno/D.S. al Coda
// pattern in a song's section ordering — modeled on LilyPond's own
// \repeat segno (checked against the LilyPond Notation Reference's
// "Segno repeat structure" documentation, not approximated from the
// iReal Pro PDF images alone): Segno marks an earlier point to jump BACK
// to, D.S. (dal segno) marks the jump-FROM point, and whatever follows
// the repeated material is the Coda, reached after the second pass. This
// is a genuinely different, BIGGER structure than \repeat volta (a
// bracket repeat of ONE section, already handled by
// GridDuplicateOf/IsReference — untouched by this function).
//
// Deliberately conservative:
//   - requires a real gap between the two runs (j > i+k) — an
//     IMMEDIATELY adjacent repeat is \repeat volta territory (the
//     existing bracket treatment), not Segno; Segno exists specifically
//     for a long-distance callback, not "play this twice in a row".
//   - requires an exact, unambiguous match of at least 2 sections —
//     returns ok=false rather than guessing at a partial/fuzzy one.
//   - prefers the LONGEST such match, and among equal lengths the one
//     whose later run reaches closest to the song's end, so what remains
//     after it reads as a real Coda tail, not most of the song.
//
// O(n^4) worst case in the section count, which is always tiny (a real
// song's section count is in the tens, not hundreds).
func detectSegnoRepeat(sg *song.Song, globalBeats int) (segnoMatch, bool) {
	n := len(sg.Sections)
	if n < 4 { // need room for a 2-section run twice, with a gap between
		return segnoMatch{}, false
	}
	beats := make([]int, n)
	for i, sec := range sg.Sections {
		beats[i] = sectionBeatsPerBar(sec, globalBeats)
	}
	var best segnoMatch
	found := false
	for k := n / 2; k >= 2; k-- {
		for i := 0; i+k <= n; i++ {
			for j := i + k + 1; j+k <= n; j++ { // j > i+k: a real gap, not back-to-back
				match := true
				for d := 0; d < k; d++ {
					if !sectionsEqual(sg.Sections[i+d], sg.Sections[j+d], beats[i+d], beats[j+d]) {
						match = false
						break
					}
				}
				if !match {
					continue
				}
				cand := segnoMatch{SegnoAt: i, DSAt: j, Len: k}
				if !found || segnoMatchBetter(cand, best, n) {
					best, found = cand, true
				}
			}
		}
	}
	return best, found
}

// segnoMatchBetter reports whether a is a better Segno/D.S. candidate
// than b: longer wins outright; among equal lengths, the one whose later
// (D.S.) run ends closer to the song's end wins, leaving a shorter, more
// plausible Coda tail rather than most of the song past the repeat.
func segnoMatchBetter(a, b segnoMatch, n int) bool {
	if a.Len != b.Len {
		return a.Len > b.Len
	}
	return (n - (a.DSAt + a.Len)) < (n - (b.DSAt + b.Len))
}

func buildSections(sg *song.Song) []viewerSection {
	globalTime := orDefault(sg.Get("time"), "4/4")
	globalBeats := timeSigBeats(globalTime)
	tempo := orDefault(sg.Get("tempo"), "120")
	num, den := timeSigParts(globalTime)
	clefDrawn := false

	var sections []viewerSection
	// dupSrc[i] is the source section index GridDuplicateOf's label was
	// taken from (-1 when section i isn't a duplicate) — kept alongside
	// `sections` only so the post-assignLabelNumbers pass below can
	// re-render that label WITH its number ("Verse 1", not bare "Verse")
	// once numbering is known, without re-running the comparison.
	var dupSrc []int
	for si, sec := range sg.Sections {
		vs := viewerSection{Index: si, Label: sec.Label, Repeat: sec.Repeat, Notes: sec.Notes, Times: sec.Times}
		beatsPerBar := sectionBeatsPerBar(sec, globalBeats)
		// A stated repeat count ("|:4 ...") is recorded on the OPEN
		// measure (see song.Measure.RepeatCount's doc comment — that's
		// where the source states it), but the "x4" label itself prints
		// on the CLOSE side instead: real engraving (checked against
		// LilyPond's own convention) places a repeat count at the END of
		// the repeated span, not the start — the opening barline just
		// marks where to jump back to, the closing one is where the
		// player needs telling "...and do that N times total."
		pendingBracketRepeatCount := 0
		for mi, m := range sec.Measures {
			if m.Open && m.RepeatCount > 0 {
				pendingBracketRepeatCount = m.RepeatCount
			}
			isRepeat := len(m.Tokens) == 1 && (m.Tokens[0] == "%" || m.Tokens[0] == "%%")
			repeatCount := 1
			if isRepeat && m.Tokens[0] == "%%" {
				repeatCount = 2
			}
			isRest := len(m.Tokens) == 1 && m.Tokens[0] == "R"
			// A volta bracket should read as one continuous ending over
			// however many bars share it, not a separate bracket per bar —
			// so only the first bar of a run gets the "1." label (see the
			// matching border-left suppression in app.css).
			voltaFirst := m.Volta != "" && (mi == 0 || sec.Measures[mi-1].Volta != m.Volta)
			vm := viewerMeasure{
				// GlobalBar is a placeholder here — real sequential numbers
				// are assigned afterward, in the dedicated pass below, once
				// every section's true bar count (including a bare/
				// reference section's inherited one) is known. See that
				// pass's own comment for why this can't just be a running
				// counter incremented in this loop.
				Index: mi, Beats: expandBeats(m.Tokens, beatsPerBar), IsRepeat: isRepeat, RepeatCount: repeatCount, IsRest: isRest,
				Volta: m.Volta, VoltaFirst: voltaFirst, Meter: m.Meter, Open: m.Open, Close: m.Close,
			}
			if m.Close && pendingBracketRepeatCount > 0 {
				vm.BracketRepeatCount = pendingBracketRepeatCount
				pendingBracketRepeatCount = 0
			}
			if !clefDrawn {
				vm.Clef = true
				vm.Tempo, vm.TimeNum, vm.TimeDen = tempo, num, den
				clefDrawn = true
			}
			vs.Measures = append(vs.Measures, vm)
		}
		vs.BarCount = realBarCount(sec.Measures)
		if written, ok := simpleRepeatSpan(sec.Measures); ok {
			vs.RepeatSegmentBars = written
		}
		// A section's own grid, played through "times:" N times (distinct
		// from a "|: ... :|" bracket WITHIN the grid, which simpleRepeatSpan
		// above already accounts for) — the written grid's one-pass length
		// becomes the side-by-side multi-rest segment length (same display
		// as a literal repeat bracket), and BarCount scales by N for the
		// song-map/structure view and the metronome's real timing.
		if sec.Times > 1 && len(sec.Measures) > 0 {
			if vs.RepeatSegmentBars == 0 {
				vs.RepeatSegmentBars = vs.BarCount
			}
			vs.BarCount *= sec.Times
		}
		// A duplicate grid: an EARLIER section (already appended to
		// `sections`, since this loop processes in order) wrote the exact
		// same measures this one just did. Compared on the raw, pre-beat-
		// expansion song.Measure data (not vs.Measures — that carries the
		// song's one-time Clef/Tempo stamp on whichever measure happened
		// to render first, which would make two genuinely identical grids
		// compare unequal). The FIRST such match wins, not just the
		// nearest — "same as Intro" should point at the grid's original
		// statement, not whichever repeat happened to come right before it.
		dupIdx := -1
		if len(sec.Measures) > 0 {
			for j := 0; j < si; j++ {
				other := sg.Sections[j]
				if len(other.Measures) > 0 && measuresEqual(other.Measures, sec.Measures, sectionBeatsPerBar(other, globalBeats), beatsPerBar) {
					vs.GridDuplicateOf = sections[j].Label
					dupIdx = j
					break
				}
			}
		}
		// A section now known to be reused verbatim by a later one (dupIdx
		// >= 0 above once resolved) gets real repeat-barline glyphs on its
		// OWN grid — the open mark on its first measure, close on its last —
		// the same .bar-open/.bar-close/.repeat-dots convention the real
		// |: ... :| source syntax already drives (play.html). This is
		// display-only: it mutates the already-built viewerMeasure copies in
		// `sections`, never sg.Sections itself, so nothing is written back
		// to the parsed song or the .song file. Lines were sliced from this
		// same Measures backing array in groupMeasures above, so the edit is
		// visible there too without re-running it.
		if dupIdx >= 0 {
			orig := sections[dupIdx].Measures
			if len(orig) > 0 {
				orig[0].Open = true
				orig[len(orig)-1].Close = true
			}
		}
		if vs.GridDuplicateOf == "" {
			vs.Lines = groupMeasures(vs.Measures, sec.BarsPerLine)
		}
		for li, l := range sec.Lyrics {
			vl := viewerLyricLine{Index: li}
			for _, seg := range l.Segments {
				vl.Segments = append(vl.Segments, viewerLyricSeg{HasChord: seg.HasChord, Chord: seg.Chord, Text: seg.Text})
			}
			vs.Lyrics = append(vs.Lyrics, vl)
		}
		sections = append(sections, vs)
		dupSrc = append(dupSrc, dupIdx)
	}
	annotateStructure(sections)
	for i := range sections {
		sec := &sections[i]
		if sec.BarCount <= 0 {
			continue
		}
		if sec.RepeatSegmentBars > 0 && sec.RepeatSegmentBars < sec.BarCount {
			sec.MultiRestSegments = sec.BarCount / sec.RepeatSegmentBars
			sec.MultiRestPer = sec.RepeatSegmentBars
		} else {
			sec.MultiRestSegments = 1
			sec.MultiRestPer = sec.BarCount
		}
	}
	assignLabelNumbers(sections)
	assignRehearsalMarks(sections)
	// Re-render each duplicate's label now that numbering is known, so two
	// same-labeled sections ("Verse 1"/"Verse 2") read unambiguously
	// ("same as Verse 1") instead of the bare label captured during the
	// main loop above, before assignLabelNumbers had run.
	for i, j := range dupSrc {
		if j < 0 {
			continue
		}
		label := sections[j].Label
		if sections[j].LabelNumber != 0 {
			label = fmt.Sprintf("%s %d", label, sections[j].LabelNumber)
		}
		sections[i].GridDuplicateOf = label
	}

	// Bar numbering — done as its own pass, after annotateStructure, not
	// interleaved with the loop above: a bare/reference section's BarCount
	// (and a Solo's inferred placeholder bar count) aren't known until
	// annotateStructure has looked at the whole structure, but every
	// section — defining or not — has to consume its real share of the
	// song's bar sequence for the clock to stay in sync with what's
	// actually being played (see StartBar's doc comment). This also
	// (re-)assigns each rendered measure's own GlobalBar sequentially
	// from its section's StartBar, replacing the placeholder values set
	// above and the copied-from-elsewhere values placeholderLines set for
	// an inferred Solo — both were wrong in exactly the cases this pass
	// exists to fix.
	globalBar := 0
	for i := range sections {
		sec := &sections[i]
		sec.StartBar = globalBar
		n := 0
		for li := range sec.Lines {
			for mi := range sec.Lines[li].Measures {
				sec.Lines[li].Measures[mi].GlobalBar = globalBar + n
				n++
			}
		}
		globalBar += sec.BarCount
	}
	if m, ok := detectSegnoRepeat(sg, globalBeats); ok {
		sections[m.SegnoAt].JazzSegno = true
		sections[m.DSAt].JazzDSAlCoda = true
		for d := 1; d < m.Len; d++ {
			sections[m.DSAt+d].JazzHidden = true
		}
		if codaAt := m.DSAt + m.Len; codaAt < len(sections) {
			sections[codaAt].JazzCoda = true
		}
	}
	return sections
}

// annotateStructure is the "compute the compact notation from the
// structure" pass: a section with its own grid is a *definition* (printed
// in full, same as a function body); a bare section (no grid, no lyrics)
// repeating an earlier definition's label is a *reference* to it (printed
// as a one-line pointer in jazz mode, the way a real chart uses a repeat
// sign instead of re-engraving the same sixteen bars). This is derived
// purely from the parsed structure already on hand — never invents a
// Coda/D.S./D.C. marker that isn't implied by an actual repeated section,
// matching how these files are really written (confirmed against the real
// .song corpus: repeated sections are routinely left bare rather than
// using the explicit "(repeat)" suffix).
// assignLabelNumbers numbers a label's occurrences ("Verse 1", "Verse 2",
// "Verse 3") whenever it appears more than once in the song — a musician
// calling out a part by name ("start from Verse 2") needs the song's own
// numbering, not source order or bar position. A label that occurs only
// once (most Intro/Bridge/Outro sections) stays unnumbered: "Intro 1"
// reads as if there's a second one coming, when there isn't.
func assignLabelNumbers(sections []viewerSection) {
	counts := map[string]int{}
	for _, sec := range sections {
		counts[strings.ToLower(strings.TrimSpace(sec.Label))]++
	}
	seen := map[string]int{}
	for i := range sections {
		key := strings.ToLower(strings.TrimSpace(sections[i].Label))
		if counts[key] <= 1 {
			continue
		}
		seen[key]++
		sections[i].LabelNumber = seen[key]
	}
}

// assignRehearsalMarks assigns each section the iReal-Pro-style rehearsal
// letter of its label: the first distinct label seen gets "A", the second
// distinct label "B", and so on — every later occurrence of a label
// (repeats, references, a numbered "Verse 2") reuses the same letter as
// its first occurrence, the same convention a real chart uses ("go back
// to B").
func assignRehearsalMarks(sections []viewerSection) {
	marks := map[string]string{}
	next := byte('A')
	for i := range sections {
		key := strings.ToLower(strings.TrimSpace(sections[i].Label))
		if key == "" {
			continue
		}
		mark, ok := marks[key]
		if !ok {
			mark = string(next)
			marks[key] = mark
			next++
		}
		sections[i].Mark = mark
	}
}

func annotateStructure(sections []viewerSection) {
	for i := range sections {
		sec := &sections[i]
		label := strings.TrimSpace(sec.Label)
		bare := len(sec.Measures) == 0 && len(sec.Lyrics) == 0
		sec.IsSolo = strings.HasPrefix(strings.ToLower(label), "solo")

		if bare {
			// "Solo" is its own convention (playing over the changes, not a
			// literal repeat of that section's own earlier self) — annotate
			// its length instead of treating it as a reference. Matched by
			// prefix, not exact equality: real charts label this kind of
			// section "Solo (Chorus)", "Solo Guitar", etc., not just the
			// bare word.
			if strings.HasPrefix(strings.ToLower(label), "solo") {
				if j := nearestDefiningSectionLabeled(sections, i, soloReferenceLabel(label)); j >= 0 {
					bars := sections[j].BarCount
					times := sec.Times
					if times < 1 {
						times = 1
					}
					unit := "bar"
					if bars*times != 1 {
						unit = "bars"
					}
					note := fmt.Sprintf("%d %s — %s changes", bars*times, unit, sections[j].Label)
					if times > 1 {
						note = fmt.Sprintf("%d %s — %s changes, x%d", bars*times, unit, sections[j].Label, times)
					}
					sec.InferredNote = note
					// Give the solo real (placeholder) bars, not just a text
					// note — otherwise it reads as a quick aside instead of
					// the chunk of the song it actually is, and a long solo
					// (several times the referenced section's length) looks
					// identical to a short one. An explicit grid: on the
					// Solo section itself still wins (it's no longer "bare"
					// in that case, so this path never runs for it) — this
					// is purely the inferred fallback.
					sec.Lines = placeholderLines(sections[j].Lines)
					sec.BarCount = bars * times
					// A "times:" > 1 bare solo reuses the referenced
					// section's own single-pass length as its repeat-segment
					// span — same side-by-side multi-rest display (two "12"
					// bars, not one "24") a literal "|: ... :|" grid gets.
					// When the referenced section is itself already a
					// repeat (RepeatSegmentBars > 0) and times is also > 1,
					// keep the referenced section's own segment span — a
					// double-nested repeat is an edge case this doesn't try
					// to represent more precisely than that.
					if times > 1 && sections[j].RepeatSegmentBars == 0 {
						sec.RepeatSegmentBars = bars
					} else {
						sec.RepeatSegmentBars = sections[j].RepeatSegmentBars
					}
				}
				continue
			}

			// Any other bare section whose label already appeared earlier
			// with real content is a reference to that definition — whether
			// or not the source bothered with the explicit "(repeat)"
			// suffix.
			for j := i - 1; j >= 0; j-- {
				if strings.EqualFold(strings.TrimSpace(sections[j].Label), label) && (len(sections[j].Measures) != 0 || len(sections[j].Lyrics) != 0) {
					sec.IsReference = true
					times := sec.Times
					if times < 1 {
						times = 1
					}
					sec.BarCount = sections[j].BarCount * times
					if times > 1 && sections[j].RepeatSegmentBars == 0 {
						sec.RepeatSegmentBars = sections[j].BarCount
					} else {
						sec.RepeatSegmentBars = sections[j].RepeatSegmentBars
					}
					// Web display only — the .song FILE still states this
					// occurrence as a bare repeat, which is deliberate: that's
					// what keeps the PDF export (internal/pdfnative, reads the
					// parsed song.Song directly, never this viewerSection
					// model) fitting on a single page. But a singer reading
					// the WEB page off a phone wants the actual words again
					// at "Chorus 2", not a silent "18 bars, repeat" — so this
					// view model (and only this one) carries the source
					// section's lyrics along, same as it already does for
					// BarCount/RepeatSegmentBars.
					sec.Lyrics = sections[j].Lyrics
					break
				}
			}
			if sec.IsReference {
				continue
			}
		}

		// A section with no grid of its own — whether bare, or carrying
		// its own lyrics but no chords — has nothing to show on a
		// chords-only chart, and previously just vanished from it: real
		// songs routinely state the chord sequence once (often under
		// Intro) and never restate it per section, which is exactly the
		// standard lead-sheet reading convention — no new chords stated
		// means the last stated changes keep running. Earlier this spelled
		// that out ("chords continue from Intro") on every such row; across
		// a real setlist that's most sections in a typical pop song, and it
		// reads as noise once seen a few times — just keep the section
		// visible as a plain chip (see Compact's doc comment) without
		// stating the obvious.
		if len(sec.Measures) == 0 {
			if j := nearestDefiningSection(sections, i); j >= 0 {
				sec.Compact = true
				times := sec.Times
				if times < 1 {
					times = 1
				}
				sec.BarCount = sections[j].BarCount * times
				if times > 1 && sections[j].RepeatSegmentBars == 0 {
					sec.RepeatSegmentBars = sections[j].BarCount
				} else {
					sec.RepeatSegmentBars = sections[j].RepeatSegmentBars
				}
			}
		}
	}
}

// placeholderLines turns a real section's chart rows into empty, dimmed
// filler rows of the same shape (same row/bar counts) — used to give a bare
// "Solo" section genuine page space proportional to what it solos over,
// instead of a one-line note. No chord text and no clef/tempo marks (those
// belong to the section being quoted, not this stand-in for it); every beat
// is a tie so it renders as dimmed rhythm slashes only.
func placeholderLines(lines []viewerLine) []viewerLine {
	out := make([]viewerLine, len(lines))
	for li, line := range lines {
		pl := viewerLine{Capacity: line.Capacity, Measures: make([]viewerMeasure, len(line.Measures))}
		for mi, m := range line.Measures {
			beats := make([]viewerBeat, len(m.Beats))
			for bi := range beats {
				beats[bi] = viewerBeat{IsTie: true}
			}
			pl.Measures[mi] = viewerMeasure{Index: mi, GlobalBar: m.GlobalBar, Beats: beats, Placeholder: true}
		}
		out[li] = pl
	}
	return out
}

// nearestDefiningSection finds the closest earlier section (by index) that
// actually has a grid — the only thing in the file implying a duration for
// a bare section that follows it.
func nearestDefiningSection(sections []viewerSection, before int) int {
	for j := before - 1; j >= 0; j-- {
		if len(sections[j].Measures) != 0 {
			return j
		}
	}
	return -1
}

// soloReferenceLabel pulls the parenthetical out of a label like
// "Solo (Chorus)" or "Solo (Bridge)" — the explicit "this solo is over
// THESE changes" the writer stated, which should win over "whichever
// section happens to be nearest" (a Solo written after a Bridge, but
// explicitly labeled "(Chorus)", solos over the Chorus, not the Bridge).
func soloReferenceLabel(label string) string {
	o := strings.IndexByte(label, '(')
	c := strings.LastIndexByte(label, ')')
	if o < 0 || c <= o {
		return ""
	}
	return strings.TrimSpace(label[o+1 : c])
}

// nearestDefiningSectionLabeled finds the nearest earlier section with a
// real grid AND a matching label — the "Solo (Chorus)" case above. Falls
// back to plain positional nearest-definition when no explicit reference
// is given, or when the referenced label never actually occurs.
func nearestDefiningSectionLabeled(sections []viewerSection, before int, wantLabel string) int {
	if wantLabel != "" {
		for j := before - 1; j >= 0; j-- {
			if len(sections[j].Measures) != 0 && strings.EqualFold(strings.TrimSpace(sections[j].Label), wantLabel) {
				return j
			}
		}
	}
	return nearestDefiningSection(sections, before)
}

// handleSongRedirect keeps old /play and /prep links working — both modes
// now live on one page (an Edit toggle switches between them), so there's
// nothing separate left to route to.
func (s *Server) handleSongRedirect(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	http.Redirect(w, r, "/b/"+book+"/"+stem, http.StatusFound)
}

// handleSong serves the unified song page: the rendered chart (formerly
// "play") plus an Edit drawer (formerly the separate "prep" page) behind a
// toggle, so editing happens in place instead of on a different URL.
func (s *Server) handleSong(w http.ResponseWriter, r *http.Request) {
	// no-store: this page's chart-rendering CSS/JS has changed several
	// times in a single day this session (stretch -> shrink-to-content ->
	// fixed-width bar cells), and a phone tab left open across those
	// deploys can get bfcache-restored by the OS/browser with the OLD
	// script execution state still live — no network round-trip at all,
	// so the /static/ cache-busting (router.go's cacheForever, keyed to
	// content hash) never even gets a chance to matter, since the HTML
	// document itself was never re-fetched. A real report matching this
	// exact symptom (bars rendering inconsistent widths within one row)
	// could not be reproduced against the current, live code at any
	// viewport/font-size tested — the CSS/JS here is correct — which
	// points at exactly this: a stale bfcache snapshot from earlier in
	// the day, not a live bug. no-store defeats bfcache eligibility in
	// every major engine (unlike most Cache-Control values, which bfcache
	// explicitly ignores), so a revisit always re-executes fresh markup
	// and script against whatever is currently deployed.
	w.Header().Set("Cache-Control", "no-store")
	book, stem := r.PathValue("book"), r.PathValue("stem")
	sg, _, err := s.store.LoadSong(book, stem)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	text, version, err := s.store.LoadRaw(book, stem)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sections := buildSections(sg)
	num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
	variantID, workID, _ := s.store.VariantLookup(r.Context(), book, stem)
	// Me: the desktop sidebar (Phase 44) needs it for the admin-only Admin
	// link and the account row, same as every other top-level page.
	person := personFromContext(r)
	// CanEdit drives the client-side implicit-fork-on-edit flow (see
	// song.js's editToggle handler): a viewer without edit rights on this
	// variant never sees a separate "fork" step — tapping Edit silently
	// forks it for them first. Same CanEditVariant check handlers_catalog.go
	// already uses for the history page; false (not an error) for the
	// legacy no-variant case, since there's nothing to fork there.
	canEdit := true
	if variantID != 0 {
		if person != nil {
			canEdit, _ = s.store.CanEditVariant(r.Context(), person.ID, variantID)
		} else {
			canEdit = false
		}
	}
	var genre string
	if workID != 0 {
		if w, err := s.store.GetWork(r.Context(), workID); err == nil {
			genre = w.Genre
		}
	}
	data := map[string]any{
		"Book": book, "ProjectName": s.store.ProjectName(r.Context(), book), "Stem": stem, "Me": person,
		"VariantID": variantID, "WorkID": workID, "CanEdit": canEdit, "Genre": genre,
		"Title": orDefault(sg.Get("title"), stem), "Artist": sg.Get("artist"),
		"Key": sg.Get("key"), "Flats": sg.Get("flats") == "yes", "Tempo": orDefault(sg.Get("tempo"), "120"), "Time": orDefault(sg.Get("time"), "4/4"),
		"TimeNum": num, "TimeDen": den,
		"Capo": sg.Get("capo"), "Reference": sg.Get("reference"),
		"TimeBeats": timeSigBeats(sg.Get("time")),
		"Sections":  sections,
		"MapLines":  buildMapLines(sections),
		"Text":      text, "Version": version,
	}
	if setlistRef := r.URL.Query().Get("setlist"); setlistRef != "" {
		data["SetlistRef"] = setlistRef
		data["Prev"], data["Next"] = s.setlistNeighbors(r.Context(), setlistRef, book, stem)
		// Arrived from the gig view (not the editable setlist page): keep the
		// back link and prev/next pointed at that read-only list instead of
		// the project page, so a reader never loses their way back to it.
		data["GigMode"] = r.URL.Query().Get("gig") == "1"
	} else {
		// Opened straight from the project page, no setlist in play: still
		// offer prev/next, from the project's own order (order.txt, or
		// alphabetical) — there's no reason a setlist should be the only
		// way to flip through a project's songs.
		data["Prev"], data["Next"] = s.projectNeighbors(book, stem)
		// Arrived from a band's own repertoire view (band_catalog.html's
		// song links carry "?band=ID") rather than the generic project
		// page — "This band is linked to the X songbook project" means
		// /b/{book} shows the SAME song list, but it's still a different
		// page than the one the reader actually came from (no band tabs,
		// no "export songbook" link, etc). Point the back-link at the
		// band's own catalog instead of silently falling through to the
		// project page in that case, same as the setlist/gig branch above
		// does for its own "where did I come from" case. Any invalid/
		// inaccessible band id is ignored, not errored — the back-link
		// just falls back to the project page as before.
		if bandParam := r.URL.Query().Get("band"); bandParam != "" {
			if bandID, err := strconv.ParseInt(bandParam, 10, 64); err == nil {
				if b, err := s.store.GetBand(r.Context(), bandID); err == nil {
					data["BandRef"] = b.ID
					data["BandName"] = b.Name
				}
			}
		}
	}
	s.render(w, "song", data)
}

// songNeighbor is the prev/next-in-setlist card: just enough to glance at
// before switching — title, tempo, key — without loading the whole chart.
type songNeighbor struct {
	Book, Stem, Title, Tempo, Key string
}

// setlistNeighbors flattens every set's entries into one running order (a
// setlist is a single gig, sets back to back) and returns the entries
// immediately before/after (book, stem) in it.
// projectNeighbors is setlistNeighbors' sibling for the no-setlist case:
// prev/next from the project's own song order (order.txt, same ordering
// project.html itself lists), so every song page offers SOME way to flip
// to the next one, not only when it was opened via a setlist.
func (s *Server) projectNeighbors(book, stem string) (prev, next *songNeighbor) {
	groups, err := s.store.ListSongsOrdered(book)
	if err != nil {
		return nil, nil
	}
	var flat []string
	at := -1
	for _, g := range groups {
		for _, st := range g.Stems {
			if st == stem && at < 0 {
				at = len(flat)
			}
			flat = append(flat, st)
		}
	}
	if at < 0 {
		return nil, nil
	}
	load := func(st string) *songNeighbor {
		sg, _, err := s.store.LoadSong(book, st)
		if err != nil {
			return nil
		}
		return &songNeighbor{Book: book, Stem: st, Title: orDefault(sg.Get("title"), st), Tempo: sg.Get("tempo"), Key: sg.Get("key")}
	}
	if at > 0 {
		prev = load(flat[at-1])
	}
	if at < len(flat)-1 {
		next = load(flat[at+1])
	}
	return prev, next
}

func (s *Server) setlistNeighbors(ctx context.Context, setlistRef, book, stem string) (prev, next *songNeighbor) {
	type ref struct{ book, stem string }
	var flat []ref
	at := -1

	// setlistRef is either "book/stem" (the legacy file-based setlist
	// family — LoadSetlist) or a bare numeric id (the DB-backed family —
	// LoadSetlistByID, the one band_catalog.html/setlist_overview.html's
	// own links and ?band= both actually use). Only the first form was
	// ever handled here — a numeric setlistRef (e.g. "?setlist=7", from
	// setlist-overview.html's own song links) silently fell through the
	// book/stem split and returned no neighbors at all, so Prev/Next
	// never appeared for a DB-backed setlist — a direct report.
	if id, err := strconv.ParseInt(setlistRef, 10, 64); err == nil {
		l, _, _, err := s.store.LoadSetlistByID(ctx, id)
		if err != nil {
			return nil, nil
		}
		for _, set := range l.Sets {
			for _, e := range set.Entries {
				eb, es := e.Book, e.Stem
				if e.IsVariant() {
					v, err := s.store.GetVariant(ctx, e.VariantID)
					if err != nil {
						continue
					}
					eb, es = v.Book, v.Stem
				}
				if eb == "" || es == "" {
					continue
				}
				if eb == book && es == stem && at < 0 {
					at = len(flat)
				}
				flat = append(flat, ref{eb, es})
			}
		}
	} else {
		parts := strings.SplitN(setlistRef, "/", 2)
		if len(parts) != 2 {
			return nil, nil
		}
		l, _, err := s.store.LoadSetlist(parts[0], parts[1])
		if err != nil {
			return nil, nil
		}
		for _, set := range l.Sets {
			for _, e := range set.Entries {
				if e.Book == book && e.Stem == stem && at < 0 {
					at = len(flat)
				}
				flat = append(flat, ref{e.Book, e.Stem})
			}
		}
	}
	if at < 0 {
		return nil, nil
	}
	load := func(r ref) *songNeighbor {
		sg, _, err := s.store.LoadSong(r.book, r.stem)
		if err != nil {
			return nil
		}
		return &songNeighbor{
			Book: r.book, Stem: r.stem, Title: orDefault(sg.Get("title"), r.stem),
			Tempo: sg.Get("tempo"), Key: sg.Get("key"),
		}
	}
	if at > 0 {
		prev = load(flat[at-1])
	}
	if at < len(flat)-1 {
		next = load(flat[at+1])
	}
	return prev, next
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// sectionBeatsPerBar is a section's own beats-per-bar: its "time:"
// override if it has one, else the song's global time signature. Factored
// out of buildSections' main loop so measuresEqual's normalized
// comparison (which needs BOTH sides' own beats-per-bar, not just the
// current loop iteration's) can compute it for an arbitrary earlier
// section too.
func sectionBeatsPerBar(sec song.Section, globalBeats int) int {
	if sec.Time != "" {
		return timeSigBeats(sec.Time)
	}
	return globalBeats
}

// timeSigBeats extracts the numerator of a "N/M" time signature (the
// metronome's beats-per-bar); defaults to 4 for anything unparsed.
func timeSigBeats(t string) int {
	i := strings.IndexByte(t, '/')
	if i <= 0 {
		return 4
	}
	n, err := strconv.Atoi(strings.TrimSpace(t[:i]))
	if err != nil || n <= 0 {
		return 4
	}
	den := strings.TrimSpace(t[i+1:])
	// Compound meter (6/8, 9/8, 12/8, ...): the beat is the dotted
	// quarter — three eighth notes — so a bar's main pulse count is
	// numerator/3, not the raw numerator. Matches beatsForSig in
	// metronome-page.js and the ACCENTS table in clock.js: all three must
	// agree on what "a beat" means for the same time signature, or the
	// chart's slash count, the metronome's click, and its accent pattern
	// would each imply a different bar.
	if den == "8" && n%3 == 0 && n >= 6 {
		return n / 3
	}
	return n
}

// timeSigParts splits "4/4" into ("4", "4") for the stacked time-signature
// badge; anything unparsed just passes the whole string through as the
// numerator with a blank denominator rather than guessing.
func timeSigParts(t string) (num, den string) {
	n, d, ok := strings.Cut(t, "/")
	if !ok {
		return t, ""
	}
	return strings.TrimSpace(n), strings.TrimSpace(d)
}

// handlePreview renders raw .song text (not necessarily saved) as a sheet
// HTML fragment, using the exact same "sheet-body" template as the play
// page — so prep's live preview can never visually drift from what playing
// the song actually looks like.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string    `json:"text"`
		Song *jsonSong `json:"song"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// The desktop focus editor's chart canvas is the structured
	// {meta, sections} shape it already edits in memory (the same wire
	// shape /api/songs/{book}/{stem}'s PUT saves), not raw .song text — it
	// has no text serializer of its own and round-tripping through one on
	// every keystroke would be pure overhead. fromJSON (json.go) already
	// exists for exactly this shape (PUT reuses it), so a second preview
	// path alongside the raw-editor's text one costs one branch, not a
	// new rendering engine: both still end at the identical buildSections
	// + "sheet-body" template call, which is the whole point — the editor
	// canvas can never visually drift from what Play actually renders.
	var sg *song.Song
	if req.Song != nil {
		sg = fromJSON(*req.Song)
	} else {
		sg = song.Parse(req.Text)
	}
	sections := buildSections(sg)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webassets.Templates.ExecuteTemplate(w, "sheet-body", map[string]any{
		"Sections": sections,
		"MapLines": buildMapLines(sections),
	}); err != nil {
		log.Printf("render preview: %v", err)
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webassets.Templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}
