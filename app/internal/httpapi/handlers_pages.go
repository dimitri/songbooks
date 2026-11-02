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
	// Compact marks a section that should render as a slim structural chip
	// even though it has no note/reference text to show — a section with no
	// grid of its own whose chords simply carry on from an earlier one.
	// Earlier this printed "chords continue from X" on every such row;
	// given real setlists this is most sections in a typical pop song, that
	// read as noise rather than information once you'd seen it a few
	// times — the chip alone (keeping the section visible in the jazz
	// chart, which is the part that actually matters) is enough.
	Compact bool
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
//   - A "|: ... :|" repeat bracket is played twice, so every bar from the
//     opening to the closing barline (inclusive) counts double. This is
//     the section's real length, used both for the song-map/structure
//     "N bars" label and for the clock's own timing (StartBar/GlobalBar
//     in buildSections below) — a repeated 8-bar phrase really does take
//     16 bars of real time to play through, and the metronome needs to
//     wait that long before moving on to the next section, not just 8.
func realBarCount(measures []song.Measure) int {
	n := 0
	firstVoltaLabel := ""
	inRepeat := false
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
		}
		mult := 1
		if inRepeat {
			mult = 2
		}
		if counts {
			n += mult
		}
		if m.Close {
			inRepeat = false
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
	return realBarCount(measures) / 2, true
}

func buildSections(sg *song.Song) []viewerSection {
	globalTime := orDefault(sg.Get("time"), "4/4")
	globalBeats := timeSigBeats(globalTime)
	tempo := orDefault(sg.Get("tempo"), "120")
	num, den := timeSigParts(globalTime)
	clefDrawn := false

	var sections []viewerSection
	for si, sec := range sg.Sections {
		vs := viewerSection{Index: si, Label: sec.Label, Repeat: sec.Repeat, Notes: sec.Notes}
		beatsPerBar := globalBeats
		if sec.Time != "" {
			beatsPerBar = timeSigBeats(sec.Time)
		}
		for mi, m := range sec.Measures {
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
		vs.Lines = groupMeasures(vs.Measures, sec.BarsPerLine)
		for li, l := range sec.Lyrics {
			vl := viewerLyricLine{Index: li}
			for _, seg := range l.Segments {
				vl.Segments = append(vl.Segments, viewerLyricSeg{HasChord: seg.HasChord, Chord: seg.Chord, Text: seg.Text})
			}
			vs.Lyrics = append(vs.Lyrics, vl)
		}
		sections = append(sections, vs)
	}
	annotateStructure(sections)
	assignLabelNumbers(sections)
	assignRehearsalMarks(sections)

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
					unit := "bar"
					if bars != 1 {
						unit = "bars"
					}
					sec.InferredNote = fmt.Sprintf("%d %s — %s changes", bars, unit, sections[j].Label)
					// Give the solo real (placeholder) bars, not just a text
					// note — otherwise it reads as a quick aside instead of
					// the chunk of the song it actually is, and a long solo
					// (several times the referenced section's length) looks
					// identical to a short one. An explicit grid: on the
					// Solo section itself still wins (it's no longer "bare"
					// in that case, so this path never runs for it) — this
					// is purely the inferred fallback.
					sec.Lines = placeholderLines(sections[j].Lines)
					sec.BarCount = sections[j].BarCount
					sec.RepeatSegmentBars = sections[j].RepeatSegmentBars
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
					sec.BarCount = sections[j].BarCount
					sec.RepeatSegmentBars = sections[j].RepeatSegmentBars
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
				sec.BarCount = sections[j].BarCount
				sec.RepeatSegmentBars = sections[j].RepeatSegmentBars
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
	// CanEdit drives the client-side implicit-fork-on-edit flow (see
	// song.js's editToggle handler): a viewer without edit rights on this
	// variant never sees a separate "fork" step — tapping Edit silently
	// forks it for them first. Same CanEditVariant check handlers_catalog.go
	// already uses for the history page; false (not an error) for the
	// legacy no-variant case, since there's nothing to fork there.
	canEdit := true
	if variantID != 0 {
		if person := personFromContext(r); person != nil {
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
		"Book": book, "Stem": stem,
		"VariantID": variantID, "WorkID": workID, "CanEdit": canEdit, "Genre": genre,
		"Title": orDefault(sg.Get("title"), stem), "Artist": sg.Get("artist"),
		"Key": sg.Get("key"), "Flats": sg.Get("flats") == "yes", "Tempo": orDefault(sg.Get("tempo"), "120"), "Time": orDefault(sg.Get("time"), "4/4"),
		"TimeNum": num, "TimeDen": den,
		"Capo": sg.Get("capo"), "Reference": sg.Get("reference"),
		"TimeBeats": timeSigBeats(sg.Get("time")),
		"Sections":  sections,
		"Text":      text, "Version": version,
	}
	if setlistRef := r.URL.Query().Get("setlist"); setlistRef != "" {
		data["SetlistRef"] = setlistRef
		data["Prev"], data["Next"] = s.setlistNeighbors(setlistRef, book, stem)
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

func (s *Server) setlistNeighbors(setlistRef, book, stem string) (prev, next *songNeighbor) {
	parts := strings.SplitN(setlistRef, "/", 2)
	if len(parts) != 2 {
		return nil, nil
	}
	l, _, err := s.store.LoadSetlist(parts[0], parts[1])
	if err != nil {
		return nil, nil
	}
	type ref struct{ book, stem string }
	var flat []ref
	at := -1
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			if e.Book == book && e.Stem == stem && at < 0 {
				at = len(flat)
			}
			flat = append(flat, ref{e.Book, e.Stem})
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
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	sg := song.Parse(req.Text)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webassets.Templates.ExecuteTemplate(w, "sheet-body", map[string]any{
		"Sections": buildSections(sg),
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
