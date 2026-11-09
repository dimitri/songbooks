package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/chordsym"
	"app/internal/song"
	"app/internal/store"
	"app/internal/tab"
	"app/internal/webassets"
)

type bookStats struct {
	ID        int64
	Name      string
	SongCount int
	SetCount  int // song_order groups (or 1 for the alphabetical fallback)
	HasOrder  bool
}

// attentionItem is one row of the home page's "needs your attention"
// feed — band proposals the viewer can vote on, plus their own unread
// notifications (proposal decisions, community submission decisions, ...).
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

// handleMarketingHome is GET / behind optionalPerson (see router.go): the
// public marketing homepage, same whether or not the visitor happens to
// be signed in — personFromContext only steers its nav between "Sign in"
// and "My Songbooks" (the latter linking to /home, the actual
// dashboard). / is never the dashboard itself, so it never has to
// choose between the pitch and your own stuff.
func (s *Server) handleMarketingHome(w http.ResponseWriter, r *http.Request) {
	s.render(w, "marketing_home", map[string]any{"Person": personFromContext(r)})
}

// handleMarketingEditor and handleMarketingEducators are the rest of the
// public marketing site (see router.go) — both optionalPerson, not
// requireAuth, so an outside link still works for a signed-out visitor,
// and a signed-in one sees "My Songbooks" in the nav instead of "Sign in".
func (s *Server) handleMarketingEditor(w http.ResponseWriter, r *http.Request) {
	s.render(w, "marketing_editor", map[string]any{"Person": personFromContext(r)})
}

// handleDocsSongFormat serves the full .song file format reference —
// header fields, grid/lyric syntax, chord diagrams, and the LilyPond-
// subset "score:" grammar (pitch/duration, hammer-on/pull-off, slides,
// this app's own bend/palm-mute text-articulation extension), plus how
// a mixed riff/chord-grid section actually renders. Linked from the
// editor marketing page and from the Source panel's own inline help
// (see song.js) so both stay pointed at one single source of truth.
func (s *Server) handleDocsSongFormat(w http.ResponseWriter, r *http.Request) {
	s.render(w, "docs_song_format", map[string]any{"Person": personFromContext(r)})
}

// docsExampleSong is the worked example at the top of /docs/song-format —
// kept here as source text (not just embedded HTML) so it stays a real,
// parseable .song document shown verbatim in the docs page's left-hand
// <pre>, with the right-hand side its ACTUAL rendering (see
// handleDocsSongFormatExample) rather than a hand-drawn approximation of
// one, which a direct report said looked visibly off from the real thing.
const docsExampleSong = `title: Just Like Heaven
artist: The Cure
key: C
time: 4/4
tempo: 120
chord: C X32010
chord: G 320033
chord: Am X02210
chord: F 133211

[Verse]
grid: | C | G | Am | F |
[C]Show me, show me, show me [G]how you do that trick
The [Am]one that makes me [F]scream, she said
`

// docsExampleTabSong/docsExampleTabLy are the docs page's SECOND worked
// example: a real note-level "score:" riff, not a hand-written one — the
// Larkin Poe cover's own intro from this app's legacy LaTeX+LilyPond
// corpus (Mercy/bad-spell/intro.ly), already a parser test fixture (see
// internal/tab/lyparse_corpus_test.go), so this is real-world LilyPond
// source, include/ChordNames noise and all, not a cleaned-up toy.
const docsExampleTabSong = `title: Bad Spell
artist: Larkin Poe
time: 4/4
tempo: 84

[Intro]
grid: | R | R | R |
score: riff.ly
`

const docsExampleTabLy = `\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\new TabStaff {
   \tabFullNotation
   \tempo 4 = 84
   \set TabStaff.minimumFret = 3
   \set TabStaff.restrainOpenStrings = ##t

   g,4 <d' g'>8. g,16 bes,8 c d f16 g
   g,4 <d' g'>8. bes16( c'4) bes8 \tuplet 3/2 { g16 f d } g,4
}

\markup \vspace #1
`

// handleDocsSongFormatExample serves the docs page's first worked
// example (a plain chord grid + chord diagrams, no score:) as its own
// tiny standalone page — see renderDocsExamplePage's doc comment for why
// this is a real page, not inlined HTML.
func (s *Server) handleDocsSongFormatExample(w http.ResponseWriter, r *http.Request) {
	sg := song.Parse(docsExampleSong)
	sections := buildSections(sg)
	s.renderDocsExamplePage(w, sg, sections)
}

// handleDocsSongFormatExampleTab serves the docs page's SECOND worked
// example: a section whose chart comes entirely from a real note-level
// "score:" riff (see docsExampleTabSong/docsExampleTabLy) rather than a
// chord grid — exercising the exact same tab.ParseScore -> attachTabRiffs
// -> alphaTab pipeline a real song's score: field goes through
// (handleSong), just with the riff supplied directly instead of fetched
// from the store, since this page has no variant/DB row behind it.
func (s *Server) handleDocsSongFormatExampleTab(w http.ResponseWriter, r *http.Request) {
	sg := song.Parse(docsExampleTabSong)
	riff, err := tab.ParseScore(docsExampleTabLy)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sections := buildSections(sg)
	num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
	attachTabRiffs(sections, map[int]*tab.Riff{0: riff}, atoiOr(num, 4), atoiOr(den, 4))
	s.renderDocsExamplePage(w, sg, sections)
}

// renderDocsExamplePage renders sections (already built by the caller,
// optionally with a score: riff attached) as its own tiny standalone
// HTML page, loaded into the docs page's <iframe> rather than inlined —
// so it renders through the exact same app.css and sheet-body template
// this app's real song page uses (see play.html/handleSong), pixel-
// identical and never hand-maintained out of sync with it, instead of a
// second, docs-only approximation of the chart's look (a direct report
// against an earlier, hand-drawn mock said it looked visibly off).
// id="sheet" matters, not just class="sheet": the 5-line staff
// background (app.css's #sheet[data-render-mode="normal"] .bar-body
// ::before, var(--staff-5lines)) is scoped to that id — omitting it was
// an earlier bug here, confirmed by a direct screenshot report ("not
// seeing the staff lines"). The page background is var(--bg), the same
// token body itself uses on a real song page — var(--bg-raised) (an
// earlier version of this function) is the card/content-box tone, not
// the page background, and visibly whiter than the real page.
func (s *Server) renderDocsExamplePage(w http.ResponseWriter, sg *song.Song, sections []viewerSection) {
	var body bytes.Buffer
	if err := webassets.Templates.ExecuteTemplate(&body, "sheet-body", map[string]any{
		"Sections": sections,
		"MapLines": buildMapLines(sections),
		"Chords":   sg.Chords,
	}); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<link rel="stylesheet" href="/static/css/app.css?v=%s">
<style>html,body{margin:0;background:var(--bg);} main.sheet{padding:16px;}</style>
</head>
<body data-mode="view">
<main class="sheet" id="sheet" data-render-mode="normal" data-key="%s">%s</main>
</body>
%s</html>`, webassets.StaticVersion(), sg.Get("key"), body.String(), docsExampleAlphaTabScripts(sections))
}

// docsExampleAlphaTabScripts loads the vendored alphaTab JS the same way
// play.html does (see its own .HasAlphaTab comment) — only when this
// particular example actually has a riff-covered row to draw, since the
// plain chord-grid example never needs the ~1.5MB library at all.
func docsExampleAlphaTabScripts(sections []viewerSection) template.HTML {
	if !anyTabOnlyLine(sections) {
		return ""
	}
	v := webassets.StaticVersion()
	return template.HTML(fmt.Sprintf(
		`<script src="/static/js/vendor/alphatab/alphaTab.min.js?v=%s"></script><script src="/static/js/alphatab-tab.js?v=%s"></script>`,
		v, v))
}

func (s *Server) handleMarketingEducators(w http.ResponseWriter, r *http.Request) {
	s.render(w, "marketing_educators", map[string]any{"Person": personFromContext(r)})
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

// buildBookStats computes song-count stats for every songbook personID
// owns, alphabetical by name. Shared by handleIndex (the home page's
// "Your songbooks" teaser) and handleCatalogPage ("by songbook" view) so
// both pages show identical stats rather than two implementations that
// can drift.
func (s *Server) buildBookStats(ctx context.Context, personID int64) ([]bookStats, error) {
	songbooks, err := s.store.ListSongbooks(ctx, "person", personID)
	if err != nil {
		return nil, err
	}
	var views []bookStats
	for _, sb := range songbooks {
		groups, err := s.store.ListSongsOrdered(ctx, sb.ID)
		if err != nil {
			return nil, err
		}
		songCount := 0
		for _, g := range groups {
			songCount += len(g.VariantIDs)
		}
		views = append(views, bookStats{
			ID: sb.ID, Name: sb.Name, SongCount: songCount, SetCount: len(groups),
			HasOrder: s.store.HasSongOrder(ctx, sb.ID),
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

	// Tab is this bar's note-level tab notation, from the section's
	// "score:" field (internal/tab) — nil for the common case of a bar
	// with no such data, which renders exactly as before. Set by
	// attachTabRiffs after buildSections, not by buildSections itself:
	// it needs a separate, optional store lookup buildSections' own
	// signature (sg *song.Song only, no variant id/ctx) doesn't have.
	Tab *viewerTabBar
	// TabOnly mirrors this bar's display row's own viewerLine.TabOnly —
	// copied onto each bar rather than looked up from the parent row so
	// the "chart-bar" template (which only ever sees one viewerMeasure,
	// not its enclosing viewerLine) can branch on it directly.
	TabOnly bool
}

// viewerTabBar is one bar's worth of tab notation. When every bar in its
// display row has one (see viewerLine.TabOnly), the row renders tab-only
// — a real string/fret staff with rhythm marks in place of the normal
// 5-line staff, chord names kept in the same position above it (see
// play.html's "chart-bar" template) — matching how a real tab chart
// looks (fret numbers ARE the notation, not an annotation on top of
// one). When only SOME bars in a row have one, those bars instead get it
// as a smaller overlay above their own normal chord/slash content — "not
// every bar needs one."
type viewerTabBar struct {
	Strings int // how many string lines to draw (len(Riff.Tuning))
	Beats   []viewerTabBeat
	// GridCols is a CSS grid-template-columns value, one "<weight>fr"
	// term per beat — real engraving spaces notes proportionally to
	// duration (a half note gets twice a quarter's horizontal room), not
	// in equal-width slots; reviewed against real LilyPond tab output.
	GridCols string
	// Beams are runs of 2+ consecutive eighth-or-shorter notes joined by
	// a horizontal beam bar instead of each drawing its own hooked flag
	// — standard notation (and real LilyPond \tabFullNotation) practice.
	// A lone flagged note with no flagged neighbor still falls back to
	// an individual hooked flag (see viewerTabBeat.Flags), same as
	// real engraving.
	Beams []viewerTabBeam
}

// viewerTabBeam is one beamed run within a bar — StartCol/EndCol are
// 1-based CSS grid-column line numbers (so "span StartCol to EndCol"
// reads directly into a grid-column CSS value), Levels is how many beam
// bars to stack (1 for an eighth-note run, 2 if the run's shortest
// member is a sixteenth, matching DurationGlyph's flag count).
type viewerTabBeam struct {
	StartCol, EndCol int
	Levels           int
}

type viewerTabBeat struct {
	Notes []viewerTabNote
	Rest  bool
	// Flags/Dotted are the rhythm mark under this beat's fret numbers —
	// real tab/Guitar-Pro/LilyPond \tabFullNotation convention: a bare
	// stem for a quarter note or longer, one flag per eighth-note
	// halving (8th=1, 16th=2, ...), a dot for a dotted duration. This is
	// the "how long does this note last" information a bare fret number
	// can't carry on its own — see internal/tab.DurationGlyph. Only
	// drawn as an individual hooked flag when NOT part of a beam group
	// (see viewerTabBar.Beams) — a beamed note still carries these so
	// the stem itself still draws, just without its own flag hooks.
	Flags  int
	Dotted bool
	// Beamed is true when this beat is part of a viewerTabBar.Beams run
	// — the template skips drawing individual hook flags for it (the
	// beam bar already shows the rhythm), drawing only its stem.
	Beamed bool
}

type viewerTabNote struct {
	String int // 1-based, top line = string 1
	Fret   int
}

// attachTabRiffs attaches each riff's bars onto the matching section's
// measures, bar index to bar index — a section's "score:" riff is
// expected to cover the SAME bars as its "grid:" (that's the whole
// point: the chord chart and the note-level riff describe the same
// passage), so riff.Bars[j] overlays sections[i].Measures[j]. A riff
// with more bars than the section has grid measures is truncated rather
// than erroring; a shorter riff just leaves the remaining bars plain —
// "optionally we have scores content for some of the bars" is exactly
// this: not every bar needs one. A display row whose every bar got one
// is additionally flagged TabOnly, switching that row's own rendering
// from the normal chord/slash staff to a tab staff (see viewerTabBar's
// doc comment) — computed per actual display row (not per section),
// since a line break can fall mid-riff.
// atoiOr parses s as an int, returning fallback on any parse failure or
// an empty string — timeSigParts's own return values are raw, unvalidated
// source text ("4", "6", possibly "" if time: was absent/malformed).
func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func attachTabRiffs(sections []viewerSection, riffs map[int]*tab.Riff, timeNum, timeDen int) {
	for si := range sections {
		riff, ok := riffs[sections[si].Index]
		if !ok {
			continue
		}
		numStrings := len(riff.Tuning)
		byBarIndex := make(map[int]*viewerTabBar, len(riff.Bars))
		rawBarByIndex := make(map[int]tab.Bar, len(riff.Bars))
		for bi, bar := range riff.Bars {
			rawBarByIndex[bi] = bar
			vb := make([]viewerTabBeat, 0, len(bar.Beats))
			weights := make([]float64, 0, len(bar.Beats))
			for _, beat := range bar.Beats {
				var notes []viewerTabNote
				for _, n := range beat.Notes {
					notes = append(notes, viewerTabNote{String: n.String, Fret: n.Fret})
				}
				flags, dotted := tab.DurationGlyph(beat.Duration)
				vb = append(vb, viewerTabBeat{Notes: notes, Rest: beat.Rest, Flags: flags, Dotted: dotted})
				w, err := tab.DurationBeats(beat.Duration)
				if err != nil || w <= 0 {
					w = 1
				}
				weights = append(weights, w)
			}
			var beams []viewerTabBeam
			for _, bg := range tab.GroupBeams(bar.Beats, timeNum, timeDen) {
				for i := bg.StartIdx; i <= bg.EndIdx; i++ {
					vb[i].Beamed = true
				}
				// CSS grid-column uses LINE numbers, not track indices:
				// track i (0-based) starts at line i+1, and spanning
				// through track j ends at line j+2.
				beams = append(beams, viewerTabBeam{StartCol: bg.StartIdx + 1, EndCol: bg.EndIdx + 2, Levels: bg.MinFlags})
			}
			byBarIndex[bi] = &viewerTabBar{Strings: numStrings, Beats: vb, GridCols: gridColsCSS(weights), Beams: beams}
		}
		// Set on both the flat Measures list AND each grouped display
		// row's own copy (groupMeasures may re-slice or copy — matching
		// by Index rather than assuming which keeps this correct either
		// way, since Index uniquely identifies a bar within its section).
		for bi := range sections[si].Measures {
			if tb, ok := byBarIndex[sections[si].Measures[bi].Index]; ok {
				sections[si].Measures[bi].Tab = tb
			}
		}
		for li := range sections[si].Lines {
			allCovered := len(sections[si].Lines[li].Measures) > 0
			for bi := range sections[si].Lines[li].Measures {
				tb, ok := byBarIndex[sections[si].Lines[li].Measures[bi].Index]
				if ok {
					sections[si].Lines[li].Measures[bi].Tab = tb
				} else {
					allCovered = false
				}
			}
			if allCovered {
				sections[si].Lines[li].TabOnly = true
				var rowBars []tab.Bar
				var chordLabels []string
				for bi := range sections[si].Lines[li].Measures {
					m := &sections[si].Lines[li].Measures[bi]
					m.TabOnly = true
					if rb, ok := rawBarByIndex[m.Index]; ok {
						rowBars = append(rowBars, rb)
					}
					label := ""
					if len(m.Beats) > 0 && !m.Beats[0].IsTie {
						label = chordsym.Glyphs(m.Beats[0].Text)
					}
					chordLabels = append(chordLabels, label)
				}
				rowRiff := &tab.Riff{Tuning: riff.Tuning, MinimumFret: riff.MinimumFret, Bars: rowBars}
				clef := ""
				if numStrings <= 4 {
					clef = "F4"
				}
				sections[si].Lines[li].AlphaTex = tab.ToAlphaTex(rowRiff, clef, chordLabels)
			}
		}
	}
}

// anyTabOnlyLine reports whether any display row across sections needs
// the vendored alphaTab JS renderer (see .HasAlphaTab, play.html) — used
// to skip loading that ~1.5MB library on a song page with no riff-
// covered row at all, the common case.
func anyTabOnlyLine(sections []viewerSection) bool {
	for _, sec := range sections {
		for _, l := range sec.Lines {
			if l.TabOnly {
				return true
			}
		}
	}
	return false
}

// gridColsCSS renders a CSS grid-template-columns value, one "<w>fr"
// term per beat's duration weight — real engraving spaces notes
// proportionally to how long they last (reviewed against real LilyPond
// tab output), not in uniform equal-width slots.
func gridColsCSS(weights []float64) string {
	var b strings.Builder
	for i, w := range weights {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.3ffr", w)
	}
	return b.String()
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
	Times  int
	Lines  []viewerLine // grouped into chart "systems" (rows), per section.BarsPerLine
	Lyrics []viewerLyricLine
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
	// TabOnly is set by attachTabRiffs when every bar in this display row
	// has note-level tab data — see viewerTabBar's doc comment for what
	// that switches in the template.
	TabOnly bool
	// AlphaTex is this row's note-level riff, re-encoded as alphaTeX
	// source for the vendored alphaTab JS renderer to draw (real
	// engraved tab, including proper beam grouping — see
	// internal/tab.ToAlphaTex's own doc comment for why this replaced
	// this app's hand-rolled CSS/SVG tab drawing for the TabOnly case).
	// Only set alongside TabOnly; "" otherwise.
	AlphaTex string
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
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	sg, _, err := s.store.LoadVariant(variantID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	text, version, err := s.store.LoadVariantRaw(variantID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sections := buildSections(sg)
	num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
	if riffs, err := s.store.LoadVariantScores(r.Context(), variantID, sg); err == nil {
		attachTabRiffs(sections, riffs, atoiOr(num, 4), atoiOr(den, 4))
	} else {
		log.Printf("loading score: riffs for variant %d: %v", variantID, err)
	}
	variant, _ := s.store.GetVariant(r.Context(), variantID)
	var workID int64
	if variant != nil {
		workID = variant.WorkID
	}
	// Me: the desktop sidebar needs it for the admin-only Admin link and
	// the account row, same as every other top-level page.
	person := personFromContext(r)
	// CanEdit drives the client-side implicit-fork-on-edit flow (see
	// song.js's editToggle handler): a viewer without edit rights on this
	// variant never sees a separate "fork" step — tapping Edit silently
	// forks it for them first.
	var canEdit bool
	if person != nil {
		canEdit, _ = s.store.CanEditVariant(r.Context(), person.ID, variantID)
	}
	var genre string
	if workID != 0 {
		if w, err := s.store.GetWork(r.Context(), workID); err == nil {
			genre = w.Genre
		}
	}
	data := map[string]any{
		"Me":        person,
		"VariantID": variantID, "WorkID": workID, "CanEdit": canEdit, "Genre": genre,
		"Title": orDefault(sg.Get("title"), "untitled"), "Artist": sg.Get("artist"),
		"Key": sg.Get("key"), "Flats": sg.Get("flats") == "yes", "Tempo": orDefault(sg.Get("tempo"), "120"), "Time": orDefault(sg.Get("time"), "4/4"),
		"TimeNum": num, "TimeDen": den,
		"Capo": sg.Get("capo"), "Reference": sg.Get("reference"),
		"TimeBeats":   timeSigBeats(sg.Get("time")),
		"Sections":    sections,
		"MapLines":    buildMapLines(sections),
		"Chords":      sg.Chords,
		"HasAlphaTab": anyTabOnlyLine(sections),
		"Text":        text, "Version": version,
	}
	if setlistRef := r.URL.Query().Get("setlist"); setlistRef != "" {
		data["SetlistRef"] = setlistRef
		data["Prev"], data["Next"] = s.setlistNeighbors(r.Context(), setlistRef, variantID)
		// Arrived from the gig view (not the editable setlist page): keep the
		// back link and prev/next pointed at that read-only list instead of
		// a songbook page, so a reader never loses their way back to it.
		data["GigMode"] = r.URL.Query().Get("gig") == "1"
	} else if songbookParam := r.URL.Query().Get("songbook"); songbookParam != "" {
		// Opened from a songbook page, no setlist in play: offer prev/next
		// from that songbook's own order.
		if songbookID, err := strconv.ParseInt(songbookParam, 10, 64); err == nil {
			data["SongbookRef"] = songbookID
			data["Prev"], data["Next"] = s.songbookNeighbors(r.Context(), songbookID, variantID)
		}
	}
	if bandParam := r.URL.Query().Get("band"); bandParam != "" {
		if bandID, err := strconv.ParseInt(bandParam, 10, 64); err == nil {
			if b, err := s.store.GetBand(r.Context(), bandID); err == nil {
				data["BandRef"] = b.ID
				data["BandName"] = b.Name
			}
		}
	}
	s.render(w, "song", data)
}

// songNeighbor is the prev/next-in-setlist card: just enough to glance at
// before switching — title, tempo, key — without loading the whole chart.
type songNeighbor struct {
	VariantID         int64
	Title, Tempo, Key string
}

func (s *Server) loadSongNeighbor(variantID int64) *songNeighbor {
	sg, _, err := s.store.LoadVariant(variantID)
	if err != nil {
		return nil
	}
	return &songNeighbor{VariantID: variantID, Title: orDefault(sg.Get("title"), "untitled"), Tempo: sg.Get("tempo"), Key: sg.Get("key")}
}

// songbookNeighbors returns the entries immediately before/after
// variantID in songbookID's own order, so every song page offers SOME
// way to flip to the next one, not only when it was opened via a
// setlist.
func (s *Server) songbookNeighbors(ctx context.Context, songbookID, variantID int64) (prev, next *songNeighbor) {
	groups, err := s.store.ListSongsOrdered(ctx, songbookID)
	if err != nil {
		return nil, nil
	}
	var flat []int64
	at := -1
	for _, g := range groups {
		for _, vid := range g.VariantIDs {
			if vid == variantID && at < 0 {
				at = len(flat)
			}
			flat = append(flat, vid)
		}
	}
	if at < 0 {
		return nil, nil
	}
	if at > 0 {
		prev = s.loadSongNeighbor(flat[at-1])
	}
	if at < len(flat)-1 {
		next = s.loadSongNeighbor(flat[at+1])
	}
	return prev, next
}

// setlistNeighbors flattens every set's entries into one running order (a
// setlist is a single gig, sets back to back) and returns the entries
// immediately before/after variantID in it.
func (s *Server) setlistNeighbors(ctx context.Context, setlistRef string, variantID int64) (prev, next *songNeighbor) {
	id, err := strconv.ParseInt(setlistRef, 10, 64)
	if err != nil {
		return nil, nil
	}
	l, _, _, err := s.store.LoadSetlistByID(ctx, id)
	if err != nil {
		return nil, nil
	}
	var flat []int64
	at := -1
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			if !e.IsVariant() {
				continue
			}
			if e.VariantID == variantID && at < 0 {
				at = len(flat)
			}
			flat = append(flat, e.VariantID)
		}
	}
	if at < 0 {
		return nil, nil
	}
	if at > 0 {
		prev = s.loadSongNeighbor(flat[at-1])
	}
	if at < len(flat)-1 {
		next = s.loadSongNeighbor(flat[at+1])
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
