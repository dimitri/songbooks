package httpapi

import (
	"app/internal/brand"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/chordsym"
	"app/internal/i18n"
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

	// MemberList is its current members, for My bands' faces.
	MemberList []store.BandMember
}

// homeGroup is a community or a band on the home page, in its own look.
type homeGroup struct {
	Community communityCardView
	Band      bandCardView
	Look      brand.Brand
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
// the attention feed, in lang — no need for anything fancier than
// day/hour/minute granularity on a feed that's capped at a handful of
// recent items.
func timeAgo(lang string, t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return i18n.T(lang, "just now")
	case d < time.Hour:
		return i18n.T(lang, "%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return i18n.T(lang, "%dh ago", int(d.Hours()))
	default:
		return i18n.T(lang, "%dd ago", int(d.Hours()/24))
	}
}

// handleMarketingHome is GET / behind optionalPerson (see router.go): the
// public marketing homepage, same whether or not the visitor happens to
// be signed in — personFromContext only steers its nav between "Sign in"
// and "My Songbooks" (the latter linking to /home, the actual
// dashboard). / is never the dashboard itself, so it never has to
// choose between the pitch and your own stuff.
func (s *Server) handleMarketingHome(w http.ResponseWriter, r *http.Request) {
	s.render(w, "public_home", publicPage(w, r, nil))
}

// handleMarketingEditor and handleMarketingEducators are the rest of the
// public marketing site (see router.go) — both optionalPerson, not
// requireAuth, so an outside link still works for a signed-out visitor,
// and a signed-in one sees "My Songbooks" in the nav instead of "Sign in".
func (s *Server) handleMarketingEditor(w http.ResponseWriter, r *http.Request) {
	s.render(w, "marketing_editor", publicPage(w, r, nil))
}

// handleMarketingGuitarPro is the dedicated /guitar-pro page: what the GP
// import wizard (gp-import.js) actually supports and how, linked from the
// editor marketing page's own "From Guitar Pro" callout.
func (s *Server) handleMarketingGuitarPro(w http.ResponseWriter, r *http.Request) {
	s.render(w, "marketing_guitar_pro", publicPage(w, r, nil))
}

// handleDocsSongFormat serves the full .song file format reference —
// header fields, grid/lyric syntax, chord diagrams, and the LilyPond-
// subset "score:" grammar (pitch/duration, hammer-on/pull-off, slides,
// this app's own bend/palm-mute text-articulation extension), plus how
// a mixed riff/chord-grid section actually renders. Linked from the
// editor marketing page and from the Source panel's own inline help
// (see song.js) so both stay pointed at one single source of truth.
func (s *Server) handleDocsSongFormat(w http.ResponseWriter, r *http.Request) {
	s.render(w, "docs_song_format", publicPage(w, r, nil))
}

type jsonSectionStructure struct {
	Index    int    `json:"index"`
	Label    string `json:"label"`
	BarCount int    `json:"barCount"`
	StartBar int    `json:"startBar"`
}

// handleGetSongStructure is GET /api/songs/{id}/structure — each
// section's REAL played bar count and starting position (repeats and
// volta alternates already expanded — realBarCount's own doc comment),
// the same numbers the chart itself renders against. Built for the GP
// import wizard (gp-import.js): it needs to map a GP file's own bars
// onto this song's EXISTING sections, and this is the one place that
// expansion math already lives — recomputing it client-side from the
// raw grid: text would be the same logic maintained twice.
func (s *Server) handleGetSongStructure(w http.ResponseWriter, r *http.Request) {
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
	sections := buildSections(sg)
	out := make([]jsonSectionStructure, len(sections))
	for i, sec := range sections {
		out[i] = jsonSectionStructure{Index: sec.Index, Label: sec.Label, BarCount: sec.BarCount, StartBar: sec.StartBar}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sections": out})
}

// handleGetDrummerRows is GET /api/songs/{id}/drummer-rows?width=<rem> —
// alphatab-tab.js's own width-aware re-fetch (see buildDrumMap's doc
// comment for the full story): the server already rendered Drummer view
// once, at a generous desktop-oriented default width, for the page's
// first paint; this reruns the SAME buildDrumMap against the width the
// client actually measured on its own .drum-map container, so a
// genuinely narrow viewport gets lines packed to actually fit it instead
// of a few, too-wide ones later squeezed down to fit by display.scale.
// width is in rem (alphatab-tab.js's own unit throughout this feature,
// matching drumRowBaseRem/drumRowRealBarRem/drumRowRestRunRem), clamped
// to a sane range rather than trusted outright — this is a public,
// unauthenticated-content-shape endpoint reachable with any query value.
func (s *Server) handleGetDrummerRows(w http.ResponseWriter, r *http.Request) {
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
	width, _ := strconv.ParseFloat(r.URL.Query().Get("width"), 64)
	switch {
	case width <= 0:
		width = defaultDrumRowTargetRem
	case width < 10:
		width = 10 // narrower than this and even a single real bar's own legibility floor can't fit — not a real device width
	case width > 200:
		width = 200 // guards against a pathological query value driving one enormous line; no real viewport is this wide in rem
	}
	sections := buildSections(sg)
	riffs, _ := tab.LoadScores(sg)
	drumMap := buildDrumMap(sections, riffs, atoiOr(orDefault(sg.Get("tempo"), "120"), 120), width)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := webassets.TemplatesFor(s.langOf(w)).ExecuteTemplate(w, "drummap_rows", drumMap); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
	}
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

// docsScoreExample is one named, live-rendered example embedded on the
// song-format docs page (and reusable from other marketing pages, e.g.
// /guitar-pro) — a real, parseable one-section .song document, shown
// verbatim in a <pre> AND rendered through the exact same
// song.Parse -> buildSections -> tab.LoadScores -> attachTabRiffs/
// buildDrumMap pipeline a real song goes through (see
// renderDocsExamplePage's own doc comment for why that matters: no
// second, docs-only approximation of the chart to drift out of sync).
// Source uses the current "score[label]:" INLINE grammar only — the
// format's old pointer-to-a-file form ("score: some.ly") was retired
// (see song/parse.go's own comment on that line) and is silently
// dropped by the real parser, so an example written that way would
// render nothing; every example here is probed against the real parser
// before being written in (see this session's own probe test).
type docsScoreExample struct {
	Source     string // the whole .song document
	Instrument string // forces attachTabRiffs' instrument-picker choice when a section has more than one melodic voice; "" picks the first
	Drummer    bool   // render via buildDrumMap (Drummer view) instead of the Song overlay — for a drum-only voice, which never shows in the Song overlay (see attachTabRiffs' pickRiff/firstMelodicRiff)
}

var docsScoreExamples = map[string]docsScoreExample{
	// The docs page's second worked example: the Larkin Poe cover's own
	// intro, written the way a human actually types it today — inline,
	// no \include/\chordmode/\new TabStaff boilerplate — rather than the
	// raw legacy .ly file (still a real parser test fixture on its own,
	// see internal/tab/lyparse_corpus_test.go, just not what this page
	// should hold up as the thing to type).
	"riff": {Source: `title: Bad Spell
artist: Larkin Poe
time: 4/4
tempo: 84

[Intro]
grid: | R | R | R |
score[guitar]: g,4 <d' g'>8. g,16 bes,8 c d f16 g
  g,4 <d' g'>8. bes16( c'4) bes8 \tuplet 3/2 { g16 f d } g,4
`},
	"bass": {Source: `title: Bass tuning
time: 4/4

[Riff]
grid: | D | D |
score[bass]: d,4 d d d d d d d
`},
	"hammer": {Source: `title: Hammer-on / pull-off
time: 4/4

[Riff]
grid: | D |
score[guitar]: \set TabStaff.minimumFret = 3
  g,4( a,) a, a,
`},
	"slide": {Source: `title: Slide
time: 4/4

[Riff]
grid: | D |
score[guitar]: f4~ f8 ees8~ \grace f16( \glissando g8)
`},
	"bend": {Source: `title: Bend and palm mute
time: 4/4

[Riff]
grid: | E |
score[guitar]: e8^"full" e8 e8^"PM" e8^"PM" e4
`},
	"drum": {Drummer: true, Source: `title: Drum notation
time: 4/4

[Chorus]
grid: |: D | % | % | % :|
score[drum]: bd4 hh8 hh8 sn4 hh8 hh8
  bd4 hh8 bd8 sn4 hh8 hh8
`},
	"multitrack": {Instrument: "bass", Source: `title: Multiple voices
time: 4/4

[Riff]
grid: | D |
score[drum]: bd4 hh8 hh8 sn4 hh8 hh8
  bd4 hh8 bd8 sn4 hh8 hh8
score[bass]: d,4 d d d d d d d
score[guitar-1]: <e, b e>4 <e b e> <e b e> <e b e>
score[guitar-2]: e'8 g e g e g e g
`},
	"mixed": {Source: `title: Mixed voice and grid
time: 4/4

[Riff]
grid: | D | D | G | G |
score[guitar]: <a, d fis>4 <a d fis> <a d fis> <a d fis>
`},
}

// handleDocsSongFormatExample serves the docs page's first worked
// example (a plain chord grid + chord diagrams, no score:) as its own
// tiny standalone page — see renderDocsExamplePage's doc comment for why
// this is a real page, not inlined HTML.
func (s *Server) handleDocsSongFormatExample(w http.ResponseWriter, r *http.Request) {
	sg := song.Parse(docsExampleSong)
	sections := buildSections(sg)
	s.renderDocsExamplePage(w, sg, sections, nil, "normal")
}

// handleDocsScoreExample serves one of docsScoreExamples (keyed by the
// "key" path value) as its own tiny standalone page, loaded into a
// docs/marketing page's <iframe> — one such example per notation
// feature documented, each a real rendering rather than a hand-drawn
// approximation.
func (s *Server) handleDocsScoreExample(w http.ResponseWriter, r *http.Request) {
	ex, ok := docsScoreExamples[r.PathValue("key")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	sg := song.Parse(ex.Source)
	sections := buildSections(sg)
	riffs, errs := tab.LoadScores(sg)
	for _, err := range errs {
		log.Printf("docs score example %q: %v", r.PathValue("key"), err)
	}
	if ex.Drummer {
		drumMap := buildDrumMap(sections, riffs, atoiOr(orDefault(sg.Get("tempo"), "120"), 120), 0)
		s.renderDocsExamplePage(w, sg, nil, drumMap, "drummer")
		return
	}
	num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
	attachTabRiffs(sections, riffs, atoiOr(num, 4), atoiOr(den, 4), sg.Get("swing"), ex.Instrument)
	s.renderDocsExamplePage(w, sg, sections, nil, "normal")
}

// renderDocsExamplePage renders sections and/or drumMap (already built
// by the caller) as its own tiny standalone HTML page, loaded into a
// docs/marketing page's <iframe> rather than inlined — so it renders
// through the exact same app.css and sheet-body template this app's
// real song page uses (see play.html/handleSong), pixel-identical and
// never hand-maintained out of sync with it, instead of a second,
// docs-only approximation of the chart's look (a direct report against
// an earlier, hand-drawn mock said it looked visibly off).
// id="sheet" matters, not just class="sheet": the 5-line staff
// background (app.css's #sheet[data-render-mode="normal"] .bar-body
// ::before, var(--staff-5lines)) is scoped to that id — omitting it was
// an earlier bug here, confirmed by a direct screenshot report ("not
// seeing the staff lines"). The page background is var(--bg), the same
// token body itself uses on a real song page — var(--bg-raised) (an
// earlier version of this function) is the card/content-box tone, not
// the page background, and visibly whiter than the real page.
//
// The trailing inline script is what actually fixes the iframe's own
// sizing (a direct report: a riff-covered example clipped mid-staff,
// showing only its first couple of bars) — the parent page's own
// onload-time height read (the iframe's own "onload" attribute, docs
// template) fires once, at DOCUMENT load, which is BEFORE alphaTab's own
// async render actually finishes mutating the DOM (it loads a ~1.5MB
// vendored JS library and draws real engraved SVG after that), so a
// height read at onload time is whatever the page measured before any
// notation was actually drawn. A ResizeObserver on #sheet, driven from
// INSIDE this same-origin child page (same origin as the parent docs
// page, so writing to window.frameElement is allowed), catches every
// later resize too — not just alphaTab's async render, but any later
// reflow — so this is the one general fix for every example this
// function ever serves, not a timing patch for one of them.
// It measures #sheet, not the document: the document is never shorter
// than the iframe itself, so an iframe once tall (a narrow first layout,
// notation still loading) would never shrink back.
func (s *Server) renderDocsExamplePage(w http.ResponseWriter, sg *song.Song, sections []viewerSection, drumMap []viewerDrumMapRow, mode string) {
	var body bytes.Buffer
	if err := webassets.Templates.ExecuteTemplate(&body, "sheet-body", map[string]any{
		"Sections": sections,
		"MapLines": buildMapLines(sections),
		"Chords":   sg.Chords,
		"DrumMap":  drumMap,
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
<main class="sheet" id="sheet" data-render-mode="%s" data-key="%s">%s</main>
<script>(function(){
  var sheet = document.getElementById('sheet');
  function resize(){ try { if (window.frameElement) window.frameElement.style.height = (Math.ceil(sheet.getBoundingClientRect().height) + 4) + 'px'; } catch (e) {} }
  if (window.ResizeObserver) new ResizeObserver(resize).observe(sheet);
  window.addEventListener('load', resize);
  resize();
})();</script>
</body>
%s</html>`, webassets.StaticVersion(), mode, sg.Get("key"), body.String(), docsExampleAlphaTabScripts(sections, drumMap))
}

// docsExampleAlphaTabScripts loads the vendored alphaTab JS the same way
// play.html does (see its own .HasAlphaTab comment) — only when this
// particular example actually has a riff-covered row (melodic) or a
// drum map (Drummer view) to draw, since a plain chord-grid example
// never needs the ~1.5MB library at all.
func docsExampleAlphaTabScripts(sections []viewerSection, drumMap []viewerDrumMapRow) template.HTML {
	if !anyTabOnlyLine(sections) && len(drumMap) == 0 {
		return ""
	}
	v := webassets.StaticVersion()
	return template.HTML(fmt.Sprintf(
		`<script src="/static/js/vendor/alphatab/alphaTab.min.js?v=%s"></script><script src="/static/js/alphatab-tab.js?v=%s"></script>`,
		v, v))
}

// publicPageHandler serves one of the public site's pages (templates/
// public_*.html): the four audience pages under /for/ and the tour.
// optionalPerson, like the home page, for the nav's "My Songbooks".
func (s *Server) publicPageHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.render(w, name, publicPage(w, r, nil))
	}
}

// handleMarketingEducators sends the old /for-educators page to its
// successor, keeping the language query.
func (s *Server) handleMarketingEducators(w http.ResponseWriter, r *http.Request) {
	target := "/for/teachers"
	if q := r.URL.RawQuery; q != "" {
		target += "?" + q
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// handleIndex is just the project directory: name + a few stats per book,
// click through to /b/{book} for the actual set list. Keeping it light
// means it stays fast and legible even with many books/projects.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)

	// Resume step for "log in to join" (see join.go's pendingJoinCookie):
	// a visitor who wasn't logged in followed a link, logged in, and has
	// just landed back here. One-shot — the cookie is cleared regardless
	// of outcome, so a failed join (link used or closed meanwhile) just
	// falls through to the ordinary dashboard rather than retrying forever.
	if cookie, err := r.Cookie(pendingJoinCookie); err == nil {
		http.SetCookie(w, &http.Cookie{Name: pendingJoinCookie, Value: "", Path: "/", MaxAge: -1})
		if dest, ok := s.resumePendingJoin(r.Context(), cookie.Value, person); ok {
			http.Redirect(w, r, dest, http.StatusFound)
			return
		}
	}

	bands, err := s.store.ListBandsForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lang := s.langOf(w)
	bandCards, attention, err := s.buildBandCards(r.Context(), lang, person.ID, bands)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// "Your communities" home teaser — same communityCardView shape
	// (member count, repertoire size, next-upcoming-event digest) the
	// /communities listing itself already builds in handleCommunitiesPage,
	// just reused here rather than duplicated: a community a person
	// belongs to was previously only reachable from the sidebar's
	// "Communities" link, not surfaced on the dashboard the way "Your
	// bands" already is.
	communityCards, err := s.buildCommunityCards(r.Context(), person.ID)
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
			Kind: "notifications", Title: n.Body, When: timeAgo(lang, n.CreatedAt), Link: n.Link,
		})
	}
	sort.SliceStable(attention, func(i, j int) bool { return i < j }) // already newest-first per source; a true merge-sort by time isn't worth it for a handful of items

	// Coming up: the next events of every band and community, from the
	// same query as the calendar page and the iCal feed, so all three
	// agree. The month itself is on /calendar.
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	soon, err := s.store.ListCalendarForPerson(r.Context(), person.ID, today, today.AddDate(0, 3, 0))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	todayCards, err := s.todayCards(r.Context(), lang, person, soon, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// The top row: the person's own songbooks beside "This week"; then
	// their bands and their communities, two rows of equal cards
	// (home.go).
	week := comingUpWeek(soon, now)
	mySongbook, err := s.mySongbook(r.Context(), lang, person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// "Recently": each song in the place it was opened from, when the
	// person may still open it (song_context.go).
	for i := range mySongbook.Recent {
		mySongbook.Recent[i].Query = s.openableSongQuery(r.Context(), person, mySongbook.Recent[i].Query)
	}
	bandGroups, communityGroups, err := s.homeGroupCards(r.Context(), lang, person.ID, bandCards, communityCards, soon, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Changes in your songs (song_news.go): news, not notifications.
	songNews, err := s.songNews(r.Context(), lang, person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.render(w, "index", map[string]any{
		"Me": person, "Greeting": homeGreeting(), "Today": todayCards,
		"Week": week, "MySongbook": mySongbook, "Bands": bandGroups, "Communities": communityGroups,
		"Attention": attention, "SongNews": songNews,
		// Arrangements that are only fixes, offered to go back to the
		// shared chart (foldback.go).
		"FoldOffers": s.homeFoldOffers(r, person.ID),
		// An invite or join link they followed no longer works
		// (dead_link.go): they're signed in, so it says so here.
		"DeadLink": r.URL.Query().Get("link") == "dead",
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
// versions that can drift. Its text is in lang.
func (s *Server) buildBandCards(ctx context.Context, lang string, personID int64, bands []store.Band) ([]bandCardView, []attentionItem, error) {
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
		changes, err := s.store.ListBandChanges(ctx, lang, b.ID, lastSeen)
		if err != nil {
			return nil, nil, err
		}
		digest := ""
		if len(changes) > 0 {
			digest = i18n.N(lang, len(changes), "%d change since your last visit", "%d changes since your last visit")
		}
		if lastSeen == nil {
			digest = i18n.T(lang, "you haven't visited yet")
		}

		proposals, err := s.store.ListProposals(ctx, b.ID, store.ProposalFilter{Status: "open", Viewer: personID})
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
			MemberList: members,
		}
		if g, ok := nextGigByBand[b.ID]; ok {
			card.HasNextGig = true
			card.NextGigDate = g.Date
			card.NextGigVenue = g.Venue
			card.NextGigSetlist = g.SetlistID
		}
		bandCards = append(bandCards, card)
		for _, p := range proposals {
			title := i18n.T(lang, "Vote: add %q to %s's songbook", p.TargetTitle, b.Name)
			if p.Kind == "new_reference" {
				title = i18n.T(lang, "Vote: new reference for %q", p.TargetTitle)
			}
			attention = append(attention, attentionItem{
				Kind: "votes", Title: title,
				Subtitle: proposalSubtitle(lang, p),
				When:     timeAgo(lang, p.CreatedAt),
				Link:     fmt.Sprintf("/bands/%d/proposals/%d", b.ID, p.ID),
			})
		}
	}
	sort.Slice(bandCards, func(i, j int) bool { return bandCards[i].Name < bandCards[j].Name })
	return bandCards, attention, nil
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
	// Pickup marks the song's very first bar when it is a pickup — an
	// anacrusis or a silent count-in bar a Guitar Pro import starts with
	// (songStartsWithPickup). The chart puts it in the first row ON TOP of
	// that row's usual bar count, so the rest keeps its natural phrasing:
	// a 16-bar blues reads 5 / 4 / 4 / 4, not ? B E B / . . . F# / ….
	// The song's "lead:" bars are pickups too, as many as it has.
	Pickup bool
	// Lead marks one of the song's "lead:" bars (song.LeadMeasures),
	// drawn on its first grid's first row ahead of bar 1, lighter, with a
	// small "lead" tag under the first. It belongs to no section: its
	// Index is negative, its GlobalBar before the first section's.
	Lead bool
	// MapAnnotate/MapMark/MapLabel are set ONLY by buildMapLines, on
	// whichever measure is the first bar of its section in Song Map's
	// flattened continuous score — every other mode's per-section
	// rendering leaves these zero-value, a no-op in "chart-bar" (see
	// play.html). This is how a section's name rides along as a small
	// floating annotation on its own first bar instead of a separate
	// element that would force a row break between sections.
	MapAnnotate       bool
	MapMark, MapLabel string

	// Played: the played bars this drawn bar is read from — each pass
	// through a repeat, the pass that takes an ending, each "times:"
	// (assignPlayedBars, playedbars.go); data-played.
	Played []int

	// TabOnly mirrors this bar's display row's own viewerLine.TabOnly —
	// copied onto each bar rather than looked up from the parent row so
	// the "chart-bar" template (which only ever sees one viewerMeasure,
	// not its enclosing viewerLine) can branch on it directly.
	TabOnly bool
}

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

// firstMelodicRiff returns a section's one melodic (non-drum) track —
// the one the main chord-chart overlay renders (a bass or guitar riff) —
// but ONLY when there's exactly one. A "score:" field with a single
// track (every file before multi-track input existed, the common case
// since, and every real catalog song using this today — Bones, Kawasaki,
// Mercy Mercy Mercy, etc., one hand-written riff per song) always
// resolves to that one track, unchanged from before NamedRiff existed.
// A section with SEVERAL melodic tracks (a full multi-instrument import)
// returns nil instead of guessing: confirmed live (Belle of the Ball, a
// full bass+guitar GP import) that auto-picking one of several tracks to
// overlay onto the shared chord chart silently replaces its chord-grid
// markup with that one track's tab notation for EVERY section — not
// just wrong for the other melodic track, but breaks Song Map and Jazz
// chart too, both of which read the same grid-replaced sections and
// render as empty/bar-count-only once the grid is gone (see
// #sheet[data-render-mode="jazz"] .sheet-section:not(.is-compact):not(
// :has(.grid)), app.css). The instrument picker (#instrument-picker,
// play.html) is the escape hatch: pickRiff below uses this function only
// when nothing was explicitly picked.
func firstMelodicRiff(tracks []tab.NamedRiff) *tab.Riff {
	var found *tab.Riff
	for _, t := range tracks {
		if t.Riff.Drums {
			continue
		}
		if found != nil {
			return nil
		}
		found = t.Riff
	}
	return found
}

// pickRiff is attachTabRiffs' own track-selection step: want empty falls
// back to firstMelodicRiff's own ambiguous-skip default (today's
// behavior, unchanged); want non-empty looks for a track whose Name
// matches it exactly (case-insensitive — a GP track's own name, e.g.
// "Electric Bass", is free text this app never rewrites) and returns nil
// if THIS section doesn't happen to carry a track by that name — no
// overlay for that one section rather than falling back to a guess, same
// as the no-score case already does.
func pickRiff(tracks []tab.NamedRiff, want string) *tab.Riff {
	if want == "" {
		return firstMelodicRiff(tracks)
	}
	for _, t := range tracks {
		if !t.Riff.Drums && strings.EqualFold(t.Name, want) {
			return t.Riff
		}
	}
	return nil
}

// collectInstrumentNames lists the distinct melodic (non-drum) track
// names across every section's score — the instrument picker's own
// option list (#instrument-picker, play.html). Sorted for a stable
// order: scoreTracks is keyed by section index and Go map iteration is
// randomized, so "first seen" isn't a meaningful order here anyway.
// mostInterestingInstrument picks, among a song's melodic score tracks,
// the one a reader most likely wants to see by default: the most varied
// line (distinct pitches weigh most, then how many notes it plays), with
// a clear preference for tracks named as the lead part (solo, lead,
// melody, theme, vocal) and against accompaniment (rhythm, comping).
// "" when the song has no melodic track.
func mostInterestingInstrument(scoreTracks map[int][]tab.NamedRiff) string {
	type stat struct {
		events  int
		pitches map[int]bool
	}
	stats := map[string]*stat{}
	for _, tracks := range scoreTracks {
		for _, t := range tracks {
			if t.Riff.Drums || t.Name == "" {
				continue
			}
			st := stats[t.Name]
			if st == nil {
				st = &stat{pitches: map[int]bool{}}
				stats[t.Name] = st
			}
			for _, bar := range t.Riff.Bars {
				for _, beat := range bar.Beats {
					if beat.Rest || len(beat.Notes) == 0 {
						continue
					}
					st.events++
					for _, n := range beat.Notes {
						st.pitches[n.MIDI*100+n.String*10+n.Fret] = true
					}
				}
			}
		}
	}
	lead := regexp.MustCompile(`(?i)solo|lead|melod|th[eè]me|chant|voix|vocal|guitar|guitare`)
	accomp := regexp.MustCompile(`(?i)rythm|rhythm|comp|accomp|bass|basse`)
	best, bestScore := "", -1.0
	for name, st := range stats {
		if st.events == 0 {
			continue
		}
		score := 2*float64(len(st.pitches)) + math.Log1p(float64(st.events))
		if lead.MatchString(name) {
			score += 20
		}
		if accomp.MatchString(name) {
			score -= 10
		}
		if score > bestScore || (score == bestScore && name < best) {
			best, bestScore = name, score
		}
	}
	return best
}

func collectInstrumentNames(scoreTracks map[int][]tab.NamedRiff) []string {
	seen := map[string]bool{}
	var out []string
	for _, tracks := range scoreTracks {
		for _, t := range tracks {
			if t.Riff.Drums || t.Name == "" || seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			out = append(out, t.Name)
		}
	}
	sort.Strings(out)
	return out
}

// chipSource is buildDrumMap's "what does this section actually draw"
// step: riff's own bars and their bar-position chord labels
// (chordsym.Glyphs(m.Beats[0].Text), skipping a tied bar — same
// extraction attachTabRiffs does for its own chord overlay) when the
// section has a matching riff, or a synthetic all-rest run (and no
// labels — nothing to hang them on) otherwise.
//
// A riff SHORTER than the section's own BarCount (a drummer writing
// just the first couple of bars of an 8-bar section, say) is padded
// with trailing rest bars rather than returned as-is: confirmed live as
// a real bug, not a hypothetical — a 2-bar riff on an 8-bar section
// rendered as a 2-bar chip with the other 6 bars simply gone, no rest
// indication at all, visibly inconsistent with that same section's own
// "8" shown everywhere else on the page (the top bar-count strip,
// Song/Song-map/Jazz-chart). A riff LONGER than BarCount (a bad save,
// not a case the UI can create today) is left untouched — truncating
// written notation would be a worse failure than showing extra bars.
func chipSource(sec *viewerSection, riff *tab.Riff) (bars []tab.Bar, chordLabels []string, restOnly bool) {
	// A pattern keeps going through every played bar, repeats included.
	riff = tab.FillPattern(riff, sec.BarCount)
	if riff != nil && len(riff.Bars) > 0 {
		bars = riff.Bars
		for len(bars) < sec.BarCount {
			bars = append(bars, tab.Bar{Beats: []tab.Beat{{Rest: true, Duration: "1"}}})
		}
		chordLabels = make([]string, 0, len(sec.Measures))
		for bi := range sec.Measures {
			m := &sec.Measures[bi]
			cl := ""
			if len(m.Beats) > 0 && !m.Beats[0].IsTie {
				cl = chordsym.Glyphs(m.Beats[0].Text)
			}
			chordLabels = append(chordLabels, cl)
		}
		return bars, chordLabels, false
	}
	bars = make([]tab.Bar, sec.BarCount)
	for i := range bars {
		bars[i] = tab.Bar{Beats: []tab.Beat{{Rest: true, Duration: "1"}}}
	}
	return bars, nil, true
}

// sectionChordBars is chipSource's own chordLabels loop (same guard, same
// chordsym.Glyphs call), lifted out so the always-visible structure strip
// (play.html) can show a section's chord sequence WITHOUT needing a riff
// at all — chipSource only ever runs for a section that has one, while a
// section with nothing but a plain chord grid (no "score:" voice, the
// common case for most of this app's songs) still has a real chord
// sequence worth showing above its bar count.
func sectionChordBars(sec *viewerSection) []string {
	bars := make([]string, 0, len(sec.Measures))
	for bi := range sec.Measures {
		m := &sec.Measures[bi]
		cl := ""
		if !m.IsRest && !m.IsRepeat && len(m.Beats) > 0 && !m.Beats[0].IsTie {
			cl = chordsym.Glyphs(m.Beats[0].Text)
		}
		bars = append(bars, cl)
	}
	return bars
}

// isWholeBarRest reports whether bar is a single whole-bar rest — the
// exact shape both chipSource's own all-rest fallback and its padding
// (a real riff shorter than its section's BarCount) produce. Used to
// tell a row's real bar count apart from its RestOnly flag (buildDrumMap):
// a section with its own real riff can still pad out with rest bars
// like this mid-row, so RestOnly (a whole SECTION having no riff at
// all) isn't enough on its own to know which of a row's bars actually
// carry notation.
func isWholeBarRest(bar tab.Bar) bool {
	return len(bar.Beats) == 1 && bar.Beats[0].Rest
}

// viewerDrumMapLabel is one section's rehearsal mark + name, floated
// above wherever that section actually starts within its line
// (viewerDrumMapRow) — LeftPct is that start bar's own position as a
// percentage of the line's bar count, the same idea Song Map's own
// chord-grid labels already use. A row usually carries one (the common
// case: a line belongs to a single section), more only where a section
// boundary falls mid-line.
type viewerDrumMapLabel struct {
	Mark, Label string
	LeftPct     float64
}

// viewerDrumMapRow is ONE engraved line of Drummer view's continuous
// sheet — see buildDrumMap's own doc comment for why it is always
// exactly one alphaTab mount, never split into several even when a
// section boundary falls mid-line. RestOnly (alphatab-tab.js, via
// data-restonly) picks which legibility floor applies to the row as a
// whole: a line built entirely from consolidated multi-bar rests needs
// much less width than one carrying real notes and chord labels would.
// RealBars (data-realbars) is how many of the row's own bars carry
// actual notation rather than a plain whole-bar rest — the real driver
// of a mixed row's min-width (alphatab-tab.js): confirmed live that
// total bar count alone is the wrong signal (an 8-bar row split 2
// real/6 rest and one split 6 real/2 rest both carry "Bars: 8", but the
// second needs far more width than the first — each real bar's own
// notation needs real room regardless of how many rest bars share the
// line, while a consolidated rest's own glyph needs the same small,
// roughly fixed amount of room no matter how many bars it folds in).
// RestRuns (data-restruns) is how many SEPARATE consolidated-rest
// glyphs the row draws — usually 0 or 1, occasionally 2 (a short rest
// run on both sides of the row's real notation). Each one needs its
// own roughly-fixed minimum width (alphatab-tab.js) regardless of how
// many bars it folds in, same reasoning as RestOnly's own doc comment,
// so the row's total floor adds one per run rather than one flat
// amount regardless of how many there are.
type viewerDrumMapRow struct {
	AlphaTex string
	Bars     int
	RealBars int
	// RealRem (data-realrem) is the room the row's written bars need
	// together, each by how busy it is (drumBarRem).
	RealRem  float64
	RestRuns int
	RestOnly bool
	Labels   []viewerDrumMapLabel
	// BarShift (data-bar-shift) is how many bars come before this row, so
	// alphatab-tab.js numbers its bars from where the song is, not from 1.
	BarShift int
}

// buildDrumMap builds Drummer view's own continuous, full-width chart —
// Song Map's own design ("one continuous engraved sheet, not disconnected
// floating chips" — see that exact commit title in this repo's history)
// applied to real drum notation instead of chord slashes: ONE sheet,
// several lines, every line the same width AND the same height, drawing
// its clef and time signature exactly ONCE, at the very start of each
// line — never per section, never per cell.
//
// Every section's bars (real notation where written, a synthetic
// all-rest run otherwise — chipSource) are flattened into one continuous
// sequence, same idea buildMapLines already uses for the chord grid,
// then chunked into lines by packDrumRowBounds — a GREEDY, WEIGHTED pack
// against targetRem (see that function's own doc comment for why a flat
// bar count, this function's previous behavior, isn't the right unit:
// confirmed live, a mobile viewport forced into the same bar count a
// wide desktop window comfortably holds either needed horizontal
// scrolling — rejected outright, this app is read hands-off while
// actually playing — or, scaled down to avoid that, rendered real
// notation small enough to misread mid-song). targetRem itself has two
// sources: handleSong's own call renders at a generous desktop-oriented
// default for the page's first paint (and for any client that never
// runs the follow-up below); alphatab-tab.js then measures the DRAWN
// .drum-map's own real width once on load and, when it's narrower,
// re-fetches this exact same rendering from handleDrummerRows at that
// real width — the same row-packing logic, just handed the width that
// actually matters, rather than a client-side re-implementation of it
// (which would mean keeping two copies of this packing logic, in two
// languages, in sync by hand) or a second, separately-computed set of
// rows baked into the initial HTML for a guessed "mobile" breakpoint
// (which still wouldn't be the device's REAL width — a tablet, a split
// view, a rotated phone all fall between any two fixed guesses).
// Where a section boundary falls MID-LINE, that line
// is STILL exactly one alphaTab mount — the whole chunk's bars go into
// ONE alphaTeX document, with a "\tempo" marker (ToAlphaTexDrums's own
// boundaries param — see its doc comment for why tempo, not alphaTeX's
// native "\section", and why it alternates tempo/tempo+1 rather than
// reusing one value) right before whichever bar starts the new section,
// so each section's own run of bars still consolidates into its own
// clean, separately-readable multi-bar-rest count instead of silently
// merging into its neighbor's.
//
// This replaces two earlier, each-wrong-in-its-own-way versions: one
// mount per SECTION regardless of line-sharing drew a separate
// clef/time-signature per section (visibly taller wherever a multi-bar
// riff's own internal wrap made its cell taller than its single-line
// neighbors, AND read as disconnected chips stapled together rather than
// one continuous staff — confirmed directly: "What's that shit? ... that
// looks like a proper music sheet to you? Looks like a patchwork to
// me."); one mount per LINE but split into several per-section CELLS
// sharing a row via CSS percentages fixed the taller-cell problem but
// kept the same patchwork look (every cell still drew its own clef) and
// turned out to need three separate, increasingly narrow CSS fixes
// (flex-basis vs. the row's own gap, html/template's CSS auto-escaper
// substituting "ZgotmplZ" for an untrusted calc() value, min-width:auto
// letting an oversized rendered SVG override the intended split) before
// it packed correctly at all. One mount per LINE, always, with no
// per-section split, needs none of that: there is never a sibling cell
// to share width or wrap against, so the whole CSS side of this problem
// stops existing.
// drumRowBaseRem, drumRowRealBarRem and drumRowRestRunRem are the SAME
// per-line-element costs alphatab-tab.js's own legibility-floor formula
// already uses to size a line's rendered width (9.5 + realBars*4.5 +
// restRuns*8.5, fitted live against two real measured rows — see that
// file's own doc comment) — reused here, not reinvented, so a line this
// function decides fits a given targetRem is, by construction, the same
// line the client would size at scale 1 (no shrink needed) once it
// applies that identical formula to what actually got packed.
const (
	drumRowBaseRem    = 9.5
	drumRowRealBarRem = 4.5
	drumRowRestRunRem = 8.5
	// defaultDrumRowTargetRem is handleSong's own first-paint target — a
	// generous width comparable to what the old flat "8 raw bars" chunk
	// size typically produced on an actual desktop browser window, so a
	// client that never runs the follow-up re-fetch (JS disabled, or the
	// fetch itself fails) still gets a reasonable, if not perfectly
	// width-matched, rendering rather than an empty or broken one.
	defaultDrumRowTargetRem = 56.0
	// drumRowRemPerBeat is the extra room each written beat of a bar
	// needs: a bar of eighths with hi-hat, snare and kick on them needs
	// far more than drumRowRealBarRem, the room of a sparse bar.
	drumRowRemPerBeat = 1.25
)

// drumBarRem is the room one bar of drums needs on a line, by how many
// beats it writes out: never less than a sparse bar's drumRowRealBarRem.
func drumBarRem(bar tab.Bar) float64 {
	beats := 0
	for _, b := range bar.Beats {
		if !b.Grace {
			beats++
		}
	}
	return max(drumRowRealBarRem, 1.6+drumRowRemPerBeat*float64(beats))
}

// packDrumRowBounds decides where each printed line of Drummer view
// breaks: a GREEDY walk that keeps adding bars to the current line while
// its projected width (drumRowBaseRem/drumRowRealBarRem/
// drumRowRestRunRem, the same formula buildDrumMap's own doc comment
// points to) stays within targetRem, starting a new line as soon as the
// next bar would exceed it — never an empty line, so a single bar wider
// than targetRem on its own still gets a line of its own (alphatab-
// tab.js's own display.scale fallback is what keeps THAT pathological
// case from overflowing the real viewport; this function's job is to
// make that fallback's job as small as possible, not to guarantee it's
// never needed). This replaces an earlier flat "8 raw bars per line"
// rule that had no notion of width at all: two rows both holding 8 raw
// bars could need very different real width (2 real bars + 6 folded
// into one rest glyph vs. 6 real bars + 2 rest bars), so a fixed count
// either wasted width on a sparse row or overflowed a busy one — exactly
// backwards from what deciding line breaks BY width, in the first place,
// naturally avoids.
func packDrumRowBounds(flat []flatDrumBar, targetRem float64) [][2]int {
	var bounds [][2]int
	for start := 0; start < len(flat); {
		end := fitDrumRow(flat, start, targetRem)
		// A line of written bars holds 1, 2, 4 or 8 of them, so phrases line
		// up from one line to the next (a 4-bar groove never splits 3 + 1);
		// the last line keeps whatever is left.
		if n := end - start; end < len(flat) && n > 2 && !rowHasRest(flat[start:end]) {
			p := 1
			for p*2 <= n {
				p *= 2
			}
			end = start + p
		}
		bounds = append(bounds, [2]int{start, end})
		start = end
	}
	return bounds
}

// fitDrumRow is where a line starting at flat[start] has to end: the
// greedy walk described on packDrumRowBounds, never an empty line.
func fitDrumRow(flat []flatDrumBar, start int, targetRem float64) int {
	realRem, restRuns := 0.0, 0
	inRestRun := false
	for i := start; i < len(flat); i++ {
		fb := flat[i]
		isBoundary := fb.isSectionStart && i > start
		addReal, addRestRun := 0.0, 0
		if isWholeBarRest(fb.bar) {
			if !inRestRun || isBoundary {
				addRestRun = 1
			}
		} else {
			addReal = drumBarRem(fb.bar)
		}
		if i > start && drumRowBaseRem+realRem+addReal+float64(restRuns+addRestRun)*drumRowRestRunRem > targetRem {
			return i
		}
		realRem += addReal
		restRuns += addRestRun
		inRestRun = isWholeBarRest(fb.bar)
	}
	return len(flat)
}

func rowHasRest(row []flatDrumBar) bool {
	for _, fb := range row {
		if isWholeBarRest(fb.bar) {
			return true
		}
	}
	return false
}

// flatDrumBar is one bar of buildDrumMap's own flattened, whole-song bar
// sequence — pulled out to its own named type (rather than staying a
// func-local struct, its shape before this comment) so packDrumRowBounds
// can take it as a parameter too.
type flatDrumBar struct {
	bar               tab.Bar
	chordLabel        string
	mark, label       string
	isSectionStart    bool
	sectionIsRestOnly bool
}

func buildDrumMap(sections []viewerSection, scoreTracks map[int][]tab.NamedRiff, tempo int, targetRem float64) []viewerDrumMapRow {
	var flat []flatDrumBar
	for si := range sections {
		sec := &sections[si]
		if sec.BarCount <= 0 {
			continue
		}
		var riff *tab.Riff
		if tracks, ok := scoreTracks[sec.Index]; ok {
			for _, t := range tracks {
				if t.Riff.Drums && len(t.Riff.Bars) > 0 {
					riff = t.Riff
					break
				}
			}
		}
		bars, chordLabels, restOnly := chipSource(sec, riff)
		for bi, b := range bars {
			fb := flatDrumBar{bar: b, sectionIsRestOnly: restOnly}
			if bi < len(chordLabels) {
				fb.chordLabel = chordLabels[bi]
			}
			if bi == 0 {
				fb.isSectionStart, fb.mark, fb.label = true, sec.Mark, sec.Label
			}
			flat = append(flat, fb)
		}
	}
	if len(flat) == 0 {
		return nil
	}
	if targetRem <= 0 {
		targetRem = defaultDrumRowTargetRem
	}
	var rows []viewerDrumMapRow
	for _, bound := range packDrumRowBounds(flat, targetRem) {
		chunk := flat[bound[0]:bound[1]]
		bars := make([]tab.Bar, 0, len(chunk))
		chordLabels := make([]string, 0, len(chunk))
		boundaries := make([]bool, len(chunk))
		restOnly := true
		realBars := 0
		realRem := 0.0
		restRuns := 0
		inRestRun := false
		var labels []viewerDrumMapLabel
		for bi, fb := range chunk {
			bars = append(bars, fb.bar)
			chordLabels = append(chordLabels, fb.chordLabel)
			if !fb.sectionIsRestOnly {
				restOnly = false
			}
			isBoundary := fb.isSectionStart && bi > 0
			if isBoundary {
				boundaries[bi] = true
			}
			if isWholeBarRest(fb.bar) {
				if !inRestRun || isBoundary {
					restRuns++
					inRestRun = true
				}
			} else {
				realBars++
				realRem += drumBarRem(fb.bar)
				inRestRun = false
			}
			if fb.isSectionStart {
				labels = append(labels, viewerDrumMapLabel{
					Mark: fb.mark, Label: fb.label,
					LeftPct: 100 * float64(bi) / float64(len(chunk)),
				})
			}
		}
		rows = append(rows, viewerDrumMapRow{
			AlphaTex: tab.ToAlphaTexDrums(&tab.Riff{Drums: true, Bars: bars}, chordLabels, boundaries, tempo),
			Bars:     len(chunk),
			RealBars: realBars,
			RealRem:  realRem,
			BarShift: bound[0],
			RestRuns: restRuns,
			RestOnly: restOnly,
			Labels:   labels,
		})
	}
	return rows
}

// attachTabRiffs lays each section's riff onto its bars, bar index to
// bar index — a section's "score:" riff covers the same bars as its
// "grid:" (the chord chart and the note-level riff describe the same
// passage), so riff.Bars[j] goes with sections[i].Measures[j]. A riff
// with more bars than the section has is truncated; a shorter one covers
// the section's first bars only. Every display row the riff covers whole
// is flagged TabOnly and drawn by alphaTab as tab (AlphaTex), the grid's
// chord names on top; a row it covers in part is first cut at the riff's
// ends (splitRiffRows), so no row mixes tab and grid bars.
//
// instrument, when non-empty, is the #instrument-picker's own selection
// (play.html) — see pickRiff's own doc comment for how it overrides
// firstMelodicRiff's default ambiguous-skip behavior.
func attachTabRiffs(sections []viewerSection, scoreTracks map[int][]tab.NamedRiff, timeNum, timeDen int, swing, instrument string) {
	// A song opening with a pickup numbers it 0 (see viewerMeasure.Pickup),
	// a lead-in of several bars -1, 0.
	songPickup := 0
	for _, sec := range sections {
		if len(sec.Measures) > 0 {
			for songPickup < len(sec.Measures) && sec.Measures[songPickup].Pickup {
				songPickup++
			}
			break
		}
	}
	// The song's last section: its last row ends the song, where a riff
	// row keeps alphaTab's final barline.
	lastGrid := len(sections) - 1
	for si := range sections {
		tracks, ok := scoreTracks[sections[si].Index]
		if !ok {
			continue
		}
		riff := pickRiff(tracks, instrument)
		if riff == nil {
			continue
		}
		numStrings := len(riff.Tuning)
		rawBarByIndex := make(map[int]tab.Bar, len(riff.Bars))
		for bi, bar := range riff.Bars {
			rawBarByIndex[bi] = bar
		}
		covered := func(m viewerMeasure) bool {
			_, ok := rawBarByIndex[m.Index]
			return ok
		}
		fullyCovered := len(sections[si].Measures) > 0
		anyCovered := false
		for _, m := range sections[si].Measures {
			if covered(m) {
				anyCovered = true
			} else {
				fullyCovered = false
			}
		}
		// A lyrics-free section with no explicit "bars:" override and a
		// riff covering every one of its bars (the common GP-import
		// shape: one section, no sections/lyrics of its own) gets
		// re-chunked into ONE continuous row here, overriding whatever
		// groupMeasures' 8-bars/row default already split it into —
		// that default exists for a chord+lyrics chart's readability,
		// which doesn't apply here, and pre-chunking it would otherwise
		// force each 8-bar chunk into its own independent alphaTeX
		// "mini-score" (own bar numbering, own premature final barline —
		// see the AlphaTex assembly below) for no reason a reader of
		// this riff would want.
		if fullyCovered && !sections[si].ExplicitBarsPerLine && len(sections[si].Lyrics) == 0 {
			sections[si].Lines = groupMeasures(sections[si].Measures, []int{len(sections[si].Measures)})
		} else if anyCovered && !fullyCovered && len(sections[si].Lines) > 0 {
			// A riff over part of the section's bars (a 4-bar intro riff
			// over a 12-bar blues grid): its rows are cut where the riff
			// starts and ends, so the covered bars make whole rows drawn
			// as tab like any riff-only row, and the rest stay chord-grid
			// rows (splitRiffRows).
			sections[si].Lines = splitRiffRows(sections[si].Measures, sections[si].Lines, covered, sections[si].ExplicitBarsPerLine)
		}
		for li := range sections[si].Lines {
			allCovered := len(sections[si].Lines[li].Measures) > 0
			for _, m := range sections[si].Lines[li].Measures {
				if !covered(m) {
					allCovered = false
				}
			}
			if allCovered {
				sections[si].Lines[li].TabOnly = true
				var rowBars []tab.Bar
				var chordLabels []string
				lineBars := sections[si].Lines[li].Measures
				barMeta := make([]string, len(lineBars))
				for bi := range lineBars {
					m := &lineBars[bi]
					m.TabOnly = true
					if rb, ok := rawBarByIndex[m.Index]; ok {
						rowBars = append(rowBars, rb)
					}
					label := ""
					if !m.IsRest && !m.IsRepeat && len(m.Beats) > 0 && !m.Beats[0].IsTie {
						label = chordsym.Glyphs(m.Beats[0].Text)
					}
					chordLabels = append(chordLabels, label)
					barMeta[bi] = riffBarMeta(*m, bi == len(lineBars)-1 && !(si == lastGrid && li == len(sections[si].Lines)-1))
				}
				rowRiff := &tab.Riff{Tuning: riff.Tuning, MinimumFret: riff.MinimumFret, Bars: rowBars}
				clef := ""
				if numStrings <= 4 {
					clef = "F4"
				}
				tex := tab.ToAlphaTexBars(rowRiff, clef, chordLabels, barMeta)
				// alphaTab assumes straight 4/4: a 12/8 shuffle drawn without
				// its own time signature reads as overfull 4/4 bars.
				if meta := tab.FirstBarMeta(timeNum, timeDen, swing); meta != "" {
					tex = strings.Replace(tex, ".\n", ".\n"+meta, 1)
				}
				if line := &sections[si].Lines[li]; len(line.Measures) > 0 {
					line.BarShift = line.Measures[0].GlobalBar - songPickup
				}
				// Fixed rows of 4 bars, like the chord chart (a 12-bar blues
				// reads as three rows of four), a pickup riding along in the
				// first; alphaTab's own automatic line breaks fit however
				// many bars happen to fit (3 here, 5 there).
				if line := &sections[si].Lines[li]; len(line.Measures) > 0 {
					n := len(line.Measures)
					var systems []string
					first := 0
					if line.Measures[0].Pickup {
						// alphaTeX anacrusis (a short bar, not a full one), left
						// unnumbered like a printed pickup; alphatab-tab.js
						// renumbers the rest so the first full bar is 1.
						tex = strings.Replace(tex, ".\n", ".\n\\ac \\barnumberdisplay hide ", 1)
						systems = append(systems, strconv.Itoa(min(n, 5)))
						first = min(n, 5)
					}
					for i := first; i < n; i += 4 {
						systems = append(systems, strconv.Itoa(min(4, n-i)))
					}
					line.Systems = strings.Join(systems, ",")
				}
				sections[si].Lines[li].AlphaTex = tex
				sections[si].Lines[li].Instrument = riff.Instrument()
			}
		}
	}
}

// riffBarMeta is the alphaTeX bar metadata (tab.ToAlphaTexBars) that
// draws a riff row's bar with what the chord chart shows on it: the
// song's tempo mark, its repeat signs, and — goesOn, the row's last bar
// when the song goes on after it — a plain barline instead of the final
// one alphaTab ends every score with (each riff row is its own score).
func riffBarMeta(m viewerMeasure, goesOn bool) string {
	var meta []string
	// The song's first bar carries its tempo mark (viewerMeasure.Clef),
	// printed above it as the chord chart's first row does.
	if t, err := strconv.Atoi(strings.TrimSpace(m.Tempo)); m.Clef && err == nil && t > 0 {
		meta = append(meta, `\tempo `+strconv.Itoa(t))
	}
	if m.Open {
		meta = append(meta, `\ro`)
	}
	if m.Close {
		n := m.BracketRepeatCount
		if n < 2 {
			n = 2
		}
		meta = append(meta, `\rc `+strconv.Itoa(n))
	} else if goesOn {
		meta = append(meta, `\barlineright regular`)
	}
	return strings.Join(meta, " ")
}

// withRiffOnlyBars gives a section that has a score but no chord bars
// (an intro riff, a drum pattern written down on its own) as many empty
// bars as its first voice has, so the chart has bars to draw the score
// on. The song itself is left untouched: the copy shares everything but
// the filled sections' measures.
func withRiffOnlyBars(sg *song.Song) *song.Song {
	var out *song.Song
	for i, sec := range sg.Sections {
		if len(sec.Measures) > 0 || sec.Repeat || len(sec.Scores) == 0 {
			continue
		}
		riff, err := tab.ParseVoice(sec.Scores[0].Label, sec.Scores[0].Body)
		if err != nil || len(riff.Bars) == 0 {
			continue
		}
		if out == nil {
			cp := *sg
			cp.Sections = append([]song.Section(nil), sg.Sections...)
			out = &cp
		}
		out.Sections[i].Measures = make([]song.Measure, len(riff.Bars))
	}
	if out == nil {
		return sg
	}
	return out
}

// anyGPRef reports whether any section draws bars from the GP file.
func anyGPRef(sections []viewerSection) bool {
	for _, sec := range sections {
		if len(sec.GP) > 0 {
			return true
		}
	}
	return false
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

// anySectionHasLyrics reports whether any section actually carries lyric
// lines — used to hide the "Lyrics only" mode entirely for a song that
// has none at all (every GP import, for one: a riff has no words), same
// reasoning as anyTabOnlyLine above deciding whether to load alphaTab.
// Selecting a mode with nothing to show isn't a real mode, just an empty
// page with the normal chrome missing.
func anySectionHasLyrics(sections []viewerSection) bool {
	for _, sec := range sections {
		if len(sec.Lyrics) > 0 {
			return true
		}
	}
	return false
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
	// ChordBars is this section's own chord sequence, one entry per bar
	// in Measures, "" for a bar with no new chord (a tie, a rest, a "%"
	// simile repeat) — set by sectionChordBars, a read-only pass over
	// Measures run AFTER buildSections/attachTabRiffs (handleSong), not
	// inside buildSections itself: this is purely a DISPLAY summary for
	// the always-visible structure strip (play.html, "always show
	// sections/chords as reference" — the Mixer view's own request),
	// independent of whether the section has any tab/riff coverage at
	// all (attachTabRiffs' own chordLabels, by contrast, only exist for
	// a fully tab-covered row).
	ChordBars []string
	// ChordSummary is ChordBars' own non-empty entries, joined for the
	// structure strip's compact one-line display ("Am · C · G · Am") —
	// a separate field rather than a template-side join/filter, which Go
	// templates can't do cleanly.
	ChordSummary string
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
	// ExplicitBarsPerLine is true when the source actually wrote a
	// "bars:" line — as opposed to Lines having been grouped by the
	// view's own 8-bars/row default (groupMeasures) because none was
	// given. attachTabRiffs reads this to decide whether a lyrics-free,
	// fully riff-covered section may re-chunk itself into one continuous
	// row instead of respecting that default; an explicit "bars:" always
	// wins over that inference.
	ExplicitBarsPerLine bool
	Lyrics []viewerLyricLine
	// Notes are this section occurrence's own free-text performance
	// directions ("GTR + claviers only") — see song.Section.Notes's doc
	// comment for the full design (per-occurrence, not per-bar-row).
	Notes []string
	// InferredNote annotates a bare "Solo" section (no grid of its own —
	// the normal way these are written: "solo over the changes") with how
	// long it runs, guessed from the nearest earlier section that actually
	// has a grid — exactly what a musician reading the chart would assume.
	// The page writes it in its own language from SoloBars, SoloOf and
	// SoloTimes; InferredNote is the English, and says there's one.
	InferredNote string
	SoloBars     int    // how many bars the solo runs
	SoloOf       string // the section whose changes it plays over
	SoloTimes    int    // how many times through (1: once)
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
	// GP are this section's "gp[Track]:" lines (gpview.go); GPOnly marks a
	// section whose bars come only from the file, so its empty chord grid
	// isn't drawn.
	GP     []viewerGPRef
	GPOnly bool
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
	// GridFrom is the section whose grid this one plays: itself when it
	// writes one, the earlier section a bare repeat, a "chords continue"
	// section or an inferred Solo borrows its bars from; -1 for none.
	GridFrom int
	// LeadBars is how many of Measures are the song's "lead:" bars drawn
	// ahead of this section's (its first grid): not its bars, in no
	// BarCount — the song plays them before StartBar.
	LeadBars int
	// PlayOrder and GridSame (assignPlayedBars, playedbars.go): the
	// written bar of its grid each of its played bars is read from, and
	// the first section writing the same grid (-1 for none) — what a view
	// needs to light the drawn bar a played bar is read from.
	PlayOrder []int
	GridSame  int
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
	// TabOnly is set by attachTabRiffs when the section's riff covers
	// every bar of this display row: the row is drawn as tab by alphaTab
	// (AlphaTex), not as a chord grid.
	TabOnly bool
	// AlphaTex is this row's note-level riff, re-encoded as alphaTeX
	// source for the vendored alphaTab JS renderer to draw (real
	// engraved tab, including proper beam grouping — see
	// internal/tab.ToAlphaTex's own doc comment for why this replaced
	// this app's hand-rolled CSS/SVG tab drawing for the TabOnly case).
	// Only set alongside TabOnly; "" otherwise.
	AlphaTex string
	// Systems, for a TabOnly row, is alphaTab's bars-per-system list:
	// "4,4,4" — or "5,4,4" when the row opens with a pickup — the same
	// phrasing the chord grid gets (see viewerMeasure.Pickup).
	Systems string
	// BarShift turns alphaTab's own bar numbers (1, 2, … in every row,
	// each row being its own score) into the song's: added to each one
	// (see alphatab-tab.js). The row's first performed bar, less one in a
	// song whose pickup is bar 0 and its first full bar 1.
	BarShift int
	// Instrument is the row's riff's kind ("guitar", "bass"): alphatab-tab.js
	// draws it the way the viewer reads that instrument (tab, staff or both,
	// store.NotationPrefs).
	Instrument string
}

// Pickups is how many of the row's bars are pickups (the song's
// lead-in, viewerMeasure.Pickup): the row's regular bars share what their
// narrow cells leave (--pickups, app.css).
func (l viewerLine) Pickups() int {
	n := 0
	for _, m := range l.Measures {
		if m.Pickup {
			n++
		}
	}
	return n
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

// splitRiffRows regroups a section's display rows when a "score:" riff
// covers only some of its bars (attachTabRiffs): the rows are cut where
// the riff's coverage starts and ends, so that every row is either all
// riff — drawn as tab by alphaTab, the riff's chord names on top — or no
// riff at all, a plain chord-grid row. A 4-bar riff over a 12-bar blues
// written as one 12-bar line reads as one tab row and two grid rows,
// as the legacy LilyPond books printed it, instead of a row of bars
// half tab, half grid.
//
// Without "bars:" a covered run of bars is one row (alphaTab breaks it
// into systems of 4 itself, see viewerLine.Systems) and an uncovered run
// rows of 4-bar phrases (riffPhraseRows), a pickup riding on the first;
// with "bars:" the author's rows are kept, only cut at the riff's ends.
// bars are the section's Measures, lines its rows (groupMeasures): the
// new rows slice the same bars, so what is set on them (Played, TabOnly)
// is shared with the section's Measures, as groupMeasures' rows are.
func splitRiffRows(bars []viewerMeasure, lines []viewerLine, covered func(viewerMeasure) bool, explicit bool) []viewerLine {
	var authorCuts []int
	n := 0
	for _, l := range lines {
		n += len(l.Measures)
		authorCuts = append(authorCuts, n)
	}
	if n != len(bars) {
		return lines // not the section's bars: leave them be
	}
	row := func(from, to int) viewerLine {
		return viewerLine{Measures: bars[from:to], Capacity: to - from}
	}
	var out []viewerLine
	for start := 0; start < len(bars); {
		cov := covered(bars[start])
		end := start + 1
		for end < len(bars) && covered(bars[end]) == cov {
			end++
		}
		switch {
		case explicit:
			from := start
			for _, cut := range authorCuts {
				if cut > from && cut < end {
					out = append(out, row(from, cut))
					from = cut
				}
			}
			out = append(out, row(from, end))
		case cov:
			out = append(out, row(start, end))
		default:
			from := start
			for _, size := range riffPhraseRows(bars[start:end]) {
				out = append(out, row(from, from+size))
				from += size
			}
		}
		start = end
	}
	return out
}

// riffPhraseRows is the bar count of each row for a run of chord-grid
// bars beside a riff: 4-bar phrases, the run's opening pickups riding on
// the first, a lone last bar joining the row before it (5 → 5, 9 → 4 +
// 5) — the Song view's own rows (bar-rows.js' plan, which re-plans them
// for the screen's width).
func riffPhraseRows(bars []viewerMeasure) []int {
	pickups := 0
	for pickups < len(bars) && bars[pickups].Pickup {
		pickups++
	}
	var sizes []int
	for rest := len(bars) - pickups; rest > 0; rest -= 4 {
		sizes = append(sizes, min(4, rest))
	}
	if k := len(sizes); k > 1 && sizes[k-1] == 1 {
		sizes[k-2]++
		sizes = sizes[:k-1]
	}
	if len(sizes) == 0 {
		return []int{pickups}
	}
	sizes[0] += pickups
	return sizes
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
	// RecallStart and RecallPlayed: the section's first played bar and
	// all of them (data-global-bar, data-played) — the cell stands for
	// every bar the section plays: a click plays from there, the cycle
	// tints it. While it plays, the bars of the grid it replays light
	// (lightedBars); the cell only when the Map draws no such bars.
	RecallStart  int
	RecallPlayed string
}

type mapLine struct {
	Cells []mapCell
}

// Pickups is how many of the row's bars are the song's lead-in (see
// viewerLine.Pickups).
func (l mapLine) Pickups() int {
	n := 0
	for _, c := range l.Cells {
		if !c.IsRecall && c.Measure.Pickup {
			n++
		}
	}
	return n
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
// recalled: sec states nothing new of its own (a bare repeat, "same as
// …", a "(repeat)", chords continuing, an inferred Solo): the Map draws it
// as one recall cell, the Song view as a chip (is-compact, its grid if
// any hidden), not its bars.
func recalled(sec viewerSection) bool {
	return sec.GridDuplicateOf != "" || sec.Repeat || sec.IsReference || sec.Compact || sec.InferredNote != ""
}

func buildMapLines(sections []viewerSection) []mapLine {
	// The played bars each drawn bar lights: its section's, and those of
	// the sections the Map recalls in a cell, on the bars of the grid
	// they replay (lightedBars).
	lit := lightedBars(sections, func(i int) bool { return !recalled(sections[i]) && len(sections[i].Measures) > 0 })
	var cells []mapCell
	for si, sec := range sections {
		label := sec.Label
		if sec.LabelNumber != 0 {
			label = fmt.Sprintf("%s %d", label, sec.LabelNumber)
		}
		if recalled(sec) {
			if sec.BarCount <= 0 {
				continue // nothing to show at all (an unresolved reference)
			}
			cells = append(cells, mapCell{
				IsRecall: true, SectionStart: true, Mark: sec.Mark, Label: label,
				RecallBars: sec.BarCount, RecallOf: sec.GridDuplicateOf,
				RecallStart: sec.StartBar, RecallPlayed: joinBars(spanBars(sec.StartBar, sec.BarCount)),
			})
			continue
		}
		badge := false
		w := 0
		for _, m := range sec.Measures {
			setPlayed(&m, lit[si], w)
			if !m.Lead {
				w++
			}
			// The section starts after the song's lead bars drawn ahead
			// of it: its name rides on its own bar 1.
			if !badge && !m.Lead {
				m.MapAnnotate, m.MapMark, m.MapLabel = true, sec.Mark, label
				badge = true
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
	return len(jzExpand(measures))
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
// note, the Jazz chart's form, jazzchart.go), so
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

func buildSections(sg *song.Song) []viewerSection {
	sg = withRiffOnlyBars(sg)
	globalTime := orDefault(sg.Get("time"), "4/4")
	globalBeats := timeSigBeats(globalTime)
	tempo := orDefault(sg.Get("tempo"), "120")
	num, den := timeSigParts(globalTime)
	clefDrawn := false
	pickupSeen := false

	var sections []viewerSection
	// dupSrc[i] is the source section index GridDuplicateOf's label was
	// taken from (-1 when section i isn't a duplicate) — kept alongside
	// `sections` only so the post-assignLabelNumbers pass below can
	// re-render that label WITH its number ("Verse 1", not bare "Verse")
	// once numbering is known, without re-running the comparison.
	var dupSrc []int
	// The song's "lead:" bars, in no section: drawn ahead of its first
	// grid's bar 1, on its first row, played before it starts.
	leadMs := sg.LeadMeasures()
	leadSec := -1
	if len(leadMs) > 0 {
		leadSec = tab.FirstGridSection(sg)
	}
	for si, sec := range sg.Sections {
		vs := viewerSection{Index: si, Label: sec.Label, Repeat: sec.Repeat, Notes: sec.Notes, Times: sec.Times, GridFrom: -1}
		ms, lead := sec.Measures, 0
		if si == leadSec {
			ms, lead = append(append([]song.Measure(nil), leadMs...), sec.Measures...), len(leadMs)
		}
		vs.LeadBars = lead
		if len(sec.Measures) > 0 {
			vs.GridFrom = si
		}
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
		for mi, m := range ms {
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
			voltaFirst := m.Volta != "" && (mi == 0 || ms[mi-1].Volta != m.Volta)
			vm := viewerMeasure{
				// GlobalBar is a placeholder here — real sequential numbers
				// are assigned afterward, in the dedicated pass below, once
				// every section's true bar count (including a bare/
				// reference section's inherited one) is known. See that
				// pass's own comment for why this can't just be a running
				// counter incremented in this loop.
				Index: mi - lead, Beats: expandBeats(m.Tokens, beatsPerBar), IsRepeat: isRepeat, RepeatCount: repeatCount, IsRest: isRest,
				Volta: m.Volta, VoltaFirst: voltaFirst, Meter: m.Meter, Open: m.Open, Close: m.Close,
			}
			if m.Lead {
				// A chordless lead bar reads as a whole-bar rest.
				vm.Lead = true
				vm.IsRest = vm.IsRest || isSilentBar(m.Tokens)
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
		barsPerLine := sec.BarsPerLine
		if lead > 0 {
			// The song's lead-in: pickups, as many as the "lead:" line
			// has, riding on the first row on top of its "bars:" count.
			pickupSeen = true
			for k := 0; k < lead; k++ {
				vs.Measures[k].Pickup = true
			}
			if len(barsPerLine) == 0 {
				barsPerLine = []int{8 + lead, 8}
			} else {
				rest := barsPerLine[1:]
				if len(rest) == 0 {
					rest = barsPerLine // "bars: 4": 4 + the lead, then 4s
				}
				barsPerLine = append([]int{barsPerLine[0] + lead}, rest...)
			}
		}
		if !pickupSeen && len(vs.Measures) > 0 {
			pickupSeen = true
			if songStartsWithPickup(sg, si) {
				vs.Measures[0].Pickup = true
				// A chordless count-in bar ("?", ".") reads as what it is in
				// print: a whole-bar rest, not an unknown chord.
				if isSilentBar(sec.Measures[0].Tokens) {
					vs.Measures[0].IsRest = true
				}
				if len(barsPerLine) == 0 {
					barsPerLine = []int{9, 8} // the pickup + the default 8, then 8s
				}
			}
		}
		if vs.GridDuplicateOf == "" {
			vs.Lines = groupMeasures(vs.Measures, barsPerLine)
			vs.ExplicitBarsPerLine = len(sec.BarsPerLine) > 0
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
	//
	// The song's lead bars come first, in no section: bars 0…K-1, drawn
	// on the first grid's row; the first section starts at bar K.
	globalBar := len(leadMs)
	for i := range sections {
		sec := &sections[i]
		sec.StartBar = globalBar
		n, k := 0, 0
		for li := range sec.Lines {
			for mi := range sec.Lines[li].Measures {
				m := &sec.Lines[li].Measures[mi]
				if m.Lead {
					m.GlobalBar = k
					k++
					continue
				}
				m.GlobalBar = globalBar + n
				n++
			}
		}
		globalBar += sec.BarCount
	}
	assignPlayedBars(sg, sections)
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
					sec.SoloBars, sec.SoloOf, sec.SoloTimes = bars*times, sections[j].Label, times
					sec.GridFrom = sections[j].GridFrom
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
					sec.GridFrom = sections[j].GridFrom
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
				sec.GridFrom = sections[j].GridFrom
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
	out := make([]viewerLine, 0, len(lines))
	for _, line := range lines {
		pl := viewerLine{Capacity: line.Capacity}
		for _, m := range line.Measures {
			if m.Lead {
				// The quoted section's lead isn't played over again.
				pl.Capacity--
				continue
			}
			beats := make([]viewerBeat, len(m.Beats))
			for bi := range beats {
				beats[bi] = viewerBeat{IsTie: true}
			}
			pl.Measures = append(pl.Measures, viewerMeasure{Index: len(pl.Measures), GlobalBar: m.GlobalBar, Beats: beats, Placeholder: true})
		}
		if len(pl.Measures) > 0 {
			out = append(out, pl)
		}
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

// transposeSpellJSON is the page's spelling table for the view-only
// transpose (chordsym.TransposedSpellings), "" for a song with no key.
func transposeSpellJSON(sg *song.Song) string {
	names, keys, ok := chordsym.TransposedSpellings(sg.Get("key"), sg.Get("flats") == "yes")
	if !ok {
		return ""
	}
	b, err := json.Marshal(map[string]any{"names": names, "keys": keys})
	if err != nil {
		return ""
	}
	return string(b)
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
	// The place the song was opened from counts only when the viewer may
	// open it (song_context.go): else it's as if the address didn't say.
	r = s.withOpenableSongContext(r)
	// A lesson opened from anywhere is opened in its course (lessonview.go).
	r = s.withCourseContext(r, variantID)
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
	// The viewer's suggestion for The Big Songbook's song: they see it in
	// place, and play it (catalogue_suggestions.go), unless they asked
	// for the song as it is (?suggestion=off).
	var suggestion *catalogueSuggestion
	if p := personFromContext(r); p != nil {
		if suggestion = s.suggestionFor(r, p.ID, variantID, text); suggestion != nil && suggestion.Shown != "" && !suggestion.Off {
			text = suggestion.Shown
			sg = song.Parse(text)
		}
	}
	gp := s.loadSongGP(r.Context(), variantID)
	sections := buildSections(withGPOnlyBars(sg, gp.Facts))
	jazz := buildJazzChart(withGPOnlyBars(sg, gp.Facts), sections)
	attachGPRefs(sections, sg, gp)
	for i := range sections {
		sections[i].ChordBars = sectionChordBars(&sections[i])
		// Collapse consecutive repeats of the SAME chord into one entry —
		// a held vamp ("Dm Am Dm Am...", one bar each) reads as "Dm · Am"
		// in a real lead sheet's own shorthand, not spelled out bar by
		// bar; the structure strip has nowhere near the width for the
		// latter anyway (direct request: "a tab-like view of the chords
		// above the bars", not a full per-bar grid here).
		var dedup []string
		for _, c := range sections[i].ChordBars {
			if c != "" && (len(dedup) == 0 || dedup[len(dedup)-1] != c) {
				dedup = append(dedup, c)
			}
		}
		sections[i].ChordSummary = strings.Join(dedup, " · ")
	}
	num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
	riffs, scoreErrs := tab.LoadScores(sg)
	instrument := r.URL.Query().Get("instrument")
	// A song with no lyrics has nothing for a plain chord chart to add
	// over the score itself, so it opens on its most interesting
	// instrument (e.g. Sarbacane's Solo rather than its Rythmique);
	// ?instrument=chords still asks for the chords explicitly.
	if instrument == "" && !anySectionHasLyrics(sections) {
		instrument = mostInterestingInstrument(riffs)
	}
	attachTabRiffs(sections, riffs, atoiOr(num, 4), atoiOr(den, 4), sg.Get("swing"), instrument)
	drumMap := buildDrumMap(sections, riffs, atoiOr(orDefault(sg.Get("tempo"), "120"), 120), 0)
	for _, e := range scoreErrs {
		log.Printf("variant %d: %v", variantID, e)
	}
	variant, _ := s.store.GetVariant(r.Context(), variantID)
	var workID int64
	if variant != nil {
		workID = variant.WorkID
	}
	// Me: the desktop sidebar needs it for the admin-only Admin link and
	// the account row, same as every other top-level page.
	person := personFromContext(r)
	// What Edit does (song.js's editToggle handler), never a silent copy
	// (docs/ownership-model.md §5; store.SongRightsFor decides):
	//   - CanEdit: opens the editor on this song, saved in place (the
	//     person's own, a band's for its members, a community's for who
	//     edits its repertoire, The Big Songbook's for the site's admins).
	//   - BandOffer: a song a band of the viewer's links (in its songbook
	//     or set list, opened from there; a public song): Edit offers
	//     "Make Mercy's version" (POST /api/bands/{id}/catalog/fork/{song}),
	//     which takes the link's place, then edits that.
	//   - CommunityOffer: the same from a community's songbook, for who
	//     edits its repertoire ("Make the community's version"); in a
	//     coaching community, its coach and the students the song is
	//     given to, from its songbooks or from none (communityEditOffer).
	//   - CanCopy without CanEdit: Edit says whose song it is and offers
	//     "Make my arrangement" (POST /api/variants/{id}/fork). Never a closed
	//     songbook's song; without any of these, there's no Edit.
	// CanNewVersion (the song is the viewer's to edit, not the
	// catalogue's): "New arrangement from this", named, beside it. CanPublish:
	// make it public or private.
	var canEdit, canCopy, canNewVersion, canPublish, canDelete bool
	// canEditChart: the editor opens on this song's chart — who edits it,
	// or anyone signed in on The Big Songbook's (catalogue_edits.go: the
	// edit is weighed against its recording; the recordings, the Guitar
	// Pro file and deleting stay CanEdit's).
	var canEditChart bool
	var bandOffer *store.BandHold
	var communityOffer *songCommunityOffer
	// arrangements: on The Big Songbook's song, the arrangements the
	// viewer may make of it (the ⋯ menu, the phone's Song sheet and the
	// editor's note), the page's place first; placeBand, the arrangement
	// the page's band already has of it (opened on the shared song).
	var arrangements []arrangementOffer
	var placeBand *store.BandVersion
	var foldOffer *store.FoldCandidate
	var editOwner *store.VariantOwner
	var bandVersion *store.BandVersion
	var basedOn *songBasedOn
	if person != nil {
		rights, _ := s.store.SongRightsFor(r.Context(), person.ID, variantID)
		if rights != nil {
			canEdit = rights.Edit
			canEditChart = rights.Edit || rights.Contribute
			canPublish = rights.Publish
			// In a coaching community, New version from this is the
			// coach's: a student edits the song they're given, in place.
			canNewVersion = rights.Edit && !rights.Catalogue && !rights.Closed && !(rights.Coaching && !rights.Publish)
			// Delete this song… (the More menu): who edits and publishes
			// it, never the catalogue's from here; a confirmation says who
			// links it first (versions_sync.go).
			canDelete = rights.Edit && rights.Publish && !rights.Catalogue
		}
		if variant != nil && rights != nil {
			// The page's own songbook first: a community's songbook
			// offers the community's version, not a band's.
			communityOffer = s.communityEditOffer(r, person.ID, variant, rights)
			if communityOffer == nil {
				bandOffer = s.bandEditOffer(r, person.ID, variant, rights)
			}
		}
		// The Big Songbook's song, for whoever may fix it (everyone signed
		// in and not kept from editing it): Edit always opens the editor on
		// the shared song, whatever place the page was opened from — a
		// band's or a community's arrangement is a separate, explicit
		// choice (the ⋯ menu, the editor's note), never what Edit does
		// (docs/catalogue-edit-review.md §4).
		fixInPlace := rights != nil && rights.Catalogue && rights.Contribute
		if (bandOffer != nil || communityOffer != nil) && !fixInPlace {
			canEdit, canEditChart, canCopy = false, false, true
		} else if !canEdit && rights != nil {
			canCopy = rights.CanVersionFor(store.PersonOwner(person.ID))
			if canCopy {
				editOwner, _ = s.store.VariantOwnerOf(r.Context(), variantID)
			}
		}
		if fixInPlace && variant != nil {
			arrangements = s.arrangementOffers(r, person.ID, variant, rights, communityOffer, rights.CanVersionFor(store.PersonOwner(person.ID)))
			placeBand = s.placeBandArrangement(r, person.ID, variantID)
			bandOffer, communityOffer = nil, nil
		}
		// A copy of one's own of a band's song: the band has its version.
		if variant != nil && variant.Visibility == "user" {
			bandVersion, _ = s.store.BandVersionOf(r.Context(), person.ID, variantID)
		}
		// A band's song made from another (its version of a song it linked, a fork into the
		// band): where it came from, when the viewer can open that.
		// A catalogue song made for another recording of one (catalogue_edits.go): that one.
		if variant != nil && (variant.Visibility == "band" || variant.Visibility == "global") && variant.ForkedFromID != nil {
			if ok, _ := s.store.CanViewVariant(r.Context(), person.ID, *variant.ForkedFromID); ok {
				if orig, err := s.store.GetVariant(r.Context(), *variant.ForkedFromID); err == nil {
					basedOn = &songBasedOn{VariantID: orig.VariantID, Title: orig.Title, Artist: orig.Artist}
					if o, err := s.store.VariantOwnerOf(r.Context(), orig.VariantID); err == nil {
						basedOn.Kind, basedOn.Owner = o.Kind, o.Name
					}
				}
			}
		}
		// An arrangement of The Big Songbook's song that is only a fix:
		// offered to go back to the shared chart (foldback.go).
		if variant != nil && variant.ForkedFromID != nil && canEdit {
			if offer := s.foldOfferFor(r, person.ID, variantID); offer != nil {
				foldOffer = offer
			}
		}
		// Home's "Continue" list; a failure here mustn't cost the page. A
		// playlist showing the song beside its player isn't a visit.
		if r.URL.Query().Get("embed") != "playlist" {
			_ = s.store.RecordSongVisit(r.Context(), person.ID, variantID, songContextQuery(r.URL.Query()))
		}
	}
	var genre string
	if workID != 0 {
		if w, err := s.store.GetWork(r.Context(), workID); err == nil {
			genre = w.Genre
		}
	}
	data := map[string]any{
		"Me":        person,
		"VariantID": variantID, "WorkID": workID, "CanEdit": canEdit, "CanEditChart": canEditChart, "CanCopy": canCopy, "Genre": genre,
		"BandOffer": bandOffer, "CommunityOffer": communityOffer, "EditOwner": editOwner,
		"Arrangements": arrangements, "PlaceBandArrangement": placeBand, "FoldOffer": foldOffer, "EditOffer": editOfferKind(canEdit, canEditChart, canCopy, bandOffer, communityOffer), "BandVersion": bandVersion, "BasedOn": basedOn, "SongVisibility": variantVisibility(variant),
		"CanNewVersion": canNewVersion, "CanPublish": canPublish, "SongPublic": variant != nil && variant.Public, "VersionName": variantVersionName(variant),
		"CanDelete": canDelete,
		"Embed": r.URL.Query().Get("embed") == "playlist",
		"Title": orDefault(sg.Get("title"), "untitled"), "Artist": sg.Get("artist"),
		"Key": sg.Get("key"), "Flats": sg.Get("flats") == "yes", "SpellJSON": transposeSpellJSON(sg), "Tempo": orDefault(sg.Get("tempo"), "120"), "Time": orDefault(sg.Get("time"), "4/4"),
		"TimeNum": num, "TimeDen": den,
		"Capo": sg.Get("capo"), "Reference": sg.Get("reference"), "GPSource": sg.Get("gp_source"),
		"TimeBeats":          timeSigBeats(sg.Get("time")),
		// The tempo counts dotted quarters (song.CompoundTime): the
		// header says "♩. = 46", not a bare "46 bpm".
		"TempoCompound":      song.CompoundTime(sg.Get("time")),
		"Sections":           sections,
		"LeadIn":             sg.LeadIn(),
		"MapLines":           buildMapLines(sections),
		"Jazz":               jazz,
		"DrumMap":            drumMap,
		"Instruments":        collectInstrumentNames(riffs),
		"SelectedInstrument": instrument,
		"NotationPrefs":      s.notationPrefsFor(r),
		"Chords":             sg.Chords,
		"HasAlphaTab":        anyTabOnlyLine(sections),
		// HasAnyScore gates the Mixer tab (play.html) — deliberately NOT
		// the same as HasAlphaTab above: that one only covers the Normal
		// chart's own fully-riff-covered-row overlay case
		// (anyTabOnlyLine), while a song can carry a playable score
		// (e.g. a drum-only voice, Drummer view's own domain) without
		// ever tripping that condition — len(riffs) > 0 is the actual
		// "does this song have anything for the Mixer to play" signal.
		"HasAnyScore":        len(riffs) > 0,
		"HasLyrics":          anySectionHasLyrics(sections),
		"Text":               text, "Version": version,
	}
	// Tap tempo (play.html #tap-tempo-dialog) compares with the song's own
	// tempo, not with .Tempo's 120 for a song that has none.
	data["SongTempo"] = sg.Get("tempo")
	// The reference in the header (song_ref_head.go): without a link, a
	// band recording is the song's reference.
	hasBand := false
	if sg.Get("reference") == "" {
		if list, err := s.store.ListSongAudio(r.Context(), variantID); err == nil {
			for _, a := range list {
				hasBand = hasBand || a.Category == store.AudioBand
			}
		}
	}
	refHead := songRefHeadFor(sg.Get("reference"), hasBand, canEditChart)
	// The Big Songbook's song: open edits (catalogue_edits.go), the
	// viewer's suggestion, and for a reviewer the ones waiting for it
	// (catalogue_suggestions.go).
	s.addCatalogueEditing(r, person, variantID, data)
	if suggestion != nil {
		data["Suggestion"] = suggestion
	}
	s.addSongSuggestions(r, person, variantID, data)
	// A student's song, proposed to their coach (coach_proposals.go).
	s.addProposeTargets(r, person, variantID, data)
	// Its title, when fetched for this link; else fetched in the
	// background for the next time (reference_title.go).
	if ref := strings.TrimSpace(sg.Get("reference")); ref != "" {
		if rt, err := s.store.ReferenceTitle(r.Context(), variantID); err == nil {
			refHead.Title = rt.For(ref)
			if rt.Due(ref, refTitleRetry, time.Now()) {
				s.fillRefTitle(variantID, ref)
			}
		}
	}
	data["RefHead"] = refHead
	// The video's audio, under it (yt_audio_jobs.go): opening the song
	// fetches it when it has none; where it is, for the page's script.
	data["YTAudio"] = s.ytAudioForPage(r.Context(), variantID, canEdit)
	// The Melody view (melody_sheet.go, melody_status.go): always there;
	// until the melody can be drawn, disabled, saying why and how far
	// along it is (its script asks again while it can change).
	if variantID != 0 {
		data["HasMelody"] = true
		if ms, err := s.melodyStatus(r.Context(), variantID, false); err == nil {
			s.finishMelodyStatus(r, variantID, &ms)
			if b, err := json.Marshal(ms); err == nil {
				data["MelodyStatus"] = string(b)
				data["MelodyUnready"] = ms.State != "ready"
				data["MelodyMessage"] = ms.Message
			}
		}
	}
	data["RefTitle"] = refHead.Title
	s.songOwnershipData(r, data, variantID, variant)
	if gp.Meta != nil {
		data["GPFileName"] = gp.Meta.Filename
		data["GPFileSHA"] = gp.Meta.SHA256
		data["GPFactsMissing"] = gp.Facts == nil
		data["GPInstruments"] = string(gpTrackProgramsJSON(gp.Meta))
		data["GPOnly"] = isGPOnly(sg)
		data["HasGPView"] = isGPOnly(sg) || anyGPRef(sections)
		if canEdit {
			data["PickerSectionsJSON"] = pickerSections(sg, gp)
		}
		for _, sec := range sections {
			for _, g := range sec.GP {
				if g.Track != "" && data["FirstGPTrack"] == nil {
					data["FirstGPTrack"] = g.Track
				}
			}
		}
		if data["FirstGPTrack"] == nil && gp.Facts != nil && len(gp.Facts.Tracks) > 0 {
			data["FirstGPTrack"] = gp.Facts.Tracks[0].Name
		}
		if canEdit && gp.Facts != nil {
			kept, _ := s.store.GPDriftAcks(r.Context(), variantID)
			drift := computeDrift(sg, buildSections(withGPOnlyBars(sg, gp.Facts)), gp.Facts, kept)
			open := 0
			for _, it := range drift {
				if !it.Kept {
					open++
				}
			}
			data["GPDrift"], data["GPDriftOpen"] = drift, open
		}
	}
	if setlistRef := r.URL.Query().Get("setlist"); setlistRef != "" {
		data["SetlistRef"] = setlistRef
		data["SetlistURL"] = "/setlists/" + setlistRef
		if id, err := strconv.ParseInt(setlistRef, 10, 64); err == nil {
			if _, meta, _, err := s.store.LoadSetlistByID(r.Context(), id); err == nil {
				data["SetlistURL"] = setlistPath(meta)
				data["SetlistName"] = meta.Title
			}
		}
		data["Prev"], data["Next"], data["Place"] = s.setlistNeighbors(r.Context(), setlistRef, variantID)
		// Arrived from the gig view (not the editable setlist page): keep the
		// back link and prev/next pointed at that read-only list instead of
		// a songbook page, so a reader never loses their way back to it.
		data["GigMode"] = r.URL.Query().Get("gig") == "1"
	} else if songbookParam := r.URL.Query().Get("songbook"); songbookParam != "" {
		// Opened from a songbook page, no setlist in play: offer prev/next
		// from that songbook's own order.
		if songbookID, err := strconv.ParseInt(songbookParam, 10, 64); err == nil {
			data["SongbookRef"] = songbookID
			data["Prev"], data["Next"], data["Place"] = s.songbookNeighbors(r.Context(), songbookID, variantID)
			if sb, err := s.store.GetSongbook(r.Context(), songbookID); err == nil {
				data["SongbookName"] = sb.Name
			}
			// A student's or a group's songbook (coaching.go): the song
			// opens in the key the coach gave it there — the chart, the
			// recording — for the coach as for the student, and says whose.
			if sb, err := s.store.GetSongbook(r.Context(), songbookID); err == nil && (sb.IsStudent() || sb.Kind == store.SongbookKindGroup) && r.URL.Query().Get("key") != "original" {
				if ok, _ := s.store.CanAccessSongbook(r.Context(), personFromContext(r).ID, songbookID); ok || personFromContext(r).IsAdmin {
					if n, err := s.store.EntrySemitones(r.Context(), songbookID, variantID); err == nil {
						data["StudentKey"] = n
						data["StudentKeySet"] = true
						data["KeyContext"] = sb.Name
						if sb.IsStudent() {
							if p, err := s.store.GetPersonByID(r.Context(), sb.StudentPersonID); err == nil {
								data["KeyContext"] = p.DisplayName
							}
							// The student, in their own songbook: their key.
							data["KeyMine"] = sb.StudentPersonID == personFromContext(r).ID
						}
						// Moved: the same line as any songbook's key (whose
						// version, the key, back to the song's own), and the
						// editor's note that it edits the song in its key.
						if n != 0 {
							mine, _ := data["KeyMine"].(bool)
							data["SongbookKey"] = s.newSongbookKeyLine(r, s.langOf(w), variant, person.ID, songKeyName(sg), n, mine, sb.Name)
						}
					}
				}
			}
			// Any other songbook the person can open that keeps the song in
			// another key (songbook_keys.go): the song opens in that key,
			// unless asked for its own (?key=original).
			if sb, err := s.store.GetSongbook(r.Context(), songbookID); err == nil && !sb.IsStudent() && sb.Kind != store.SongbookKindGroup && r.URL.Query().Get("key") != "original" {
				if ok, _ := s.store.CanAccessSongbook(r.Context(), person.ID, songbookID); ok {
					if n, in, err := s.store.SongbookEntryKey(r.Context(), songbookID, variantID); err == nil && in && n != 0 {
						mine := sb.OwnerKind == "person" && sb.OwnerPersonID == person.ID && sb.IsDefault
						data["StudentKey"], data["StudentKeySet"] = n, true
						data["SongbookKey"] = s.newSongbookKeyLine(r, s.langOf(w), variant, person.ID, songKeyName(sg), n, mine, sb.Name)
					}
				}
			}
			// In a course, back is to the song's lesson.
			if sb, err := s.store.GetSongbook(r.Context(), songbookID); err == nil && sb.IsCourse() && s.mayOpenSongbook(r, sb.ID) {
				if l, err := s.store.LessonOfSong(r.Context(), songbookID, variantID); err == nil && l != nil {
					// Back is to the lesson's own page (?view=lesson: it
					// would bring the song back), and the lesson comes
					// along: its video, done, its files (lessonPanel).
					data["LessonHref"], data["LessonLabel"] = lessonHref(songbookID, l.ID)+"?view=lesson", l.Label(s.langOf(w))
					if panel, err := s.lessonPanel(r, sb, l); err == nil {
						data["LessonPanel"] = panel
					}
				}
			}
		}
	}
	// From a coaching event's program (coach_program.go): the song in the
	// key it will be sung in there, back to the program, its running order
	// for previous and next.
	if eventParam := r.URL.Query().Get("event"); eventParam != "" && data["SetlistRef"] == nil && data["SongbookRef"] == nil {
		if eventID, err := strconv.ParseInt(eventParam, 10, 64); err == nil {
			if ev, err := s.store.GetEvent(r.Context(), eventID); err == nil && ev.OwnerKind == "community" {
				member := person.IsAdmin
				if !member {
					member, _ = s.store.IsCurrentCommunityMember(r.Context(), ev.OwnerCommunityID, person.ID)
				}
				if n, ok, err := s.store.ProgramKeyOf(r.Context(), ev.ID, variantID); member && err == nil && ok {
					data["EventRef"] = ev.ID
					data["StudentKey"], data["StudentKeySet"], data["KeyContext"] = n, true, ev.Name
					data["LessonHref"], data["LessonLabel"] = fmt.Sprintf("/communities/%d/events/%d/program", ev.OwnerCommunityID, ev.ID), ev.Name
					if prog, err := s.store.ListEventProgram(r.Context(), ev.ID); err == nil {
						order := make([]int64, len(prog))
						for i, p := range prog {
							order[i] = p.VariantID
						}
						data["Prev"], data["Next"], data["Place"] = s.neighborsAt(listPlace(order, variantID))
					}
				}
			}
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
	if communityParam := r.URL.Query().Get("community"); communityParam != "" {
		if communityID, err := strconv.ParseInt(communityParam, 10, 64); err == nil {
			if _, err := s.store.GetCommunity(r.Context(), communityID); err == nil {
				data["CommunityRef"] = communityID
				// CommunityName is the community's own DEFAULT SONGBOOK
				// name, not the community's own name — a community's
				// repertoire IS a real songbook (owner_kind='community'),
				// and the reader came from browsing it by that name
				// ("La Nouvelle Méthode Blues Rock"), not by the
				// community's own name ("Florent Passamonti"); showing
				// the latter here read as the wrong/generic label.
				data["CommunityName"] = "songbook"
				if sbID, err := s.store.GetDefaultSongbook(r.Context(), "community", communityID); err == nil {
					if sb, err := s.store.GetSongbook(r.Context(), sbID); err == nil {
						data["CommunityName"] = sb.Name
					}
				}
			}
		}
	}
	// The coaching community the song was opened in (its songbooks, its
	// page, an event of its), 0 for none: a student's key ("Your key") and
	// the coach's key per student are for the songs practised there, not
	// for those played in a band, a set list or anywhere else.
	coachCtx := s.songCoachingContext(r, data)
	// There a song opens on its lyrics, following the music (song.js),
	// unless the reader picked another view for it.
	if coachCtx != 0 {
		data["CoachingContext"] = coachCtx
	}
	// A student opening one of their songs from elsewhere in that
	// community (its repertoire, a group's list, an assignment) gets it in
	// their own key too.
	if data["StudentKeySet"] == nil && coachCtx != 0 && r.URL.Query().Get("key") != "original" {
		if _, n, ok, err := s.store.StudentKeyIn(r.Context(), person.ID, coachCtx, variantID); err == nil && ok {
			data["StudentKey"], data["StudentKeySet"], data["KeyContext"], data["KeyMine"] = n, true, person.DisplayName, true
		}
	}
	// Opened from nowhere in particular (home, a search, the catalogue),
	// not from a band's, a community's, a set list's or another songbook:
	// the key the song has in the person's own songbook.
	if data["StudentKeySet"] == nil && data["SetlistRef"] == nil && data["SongbookRef"] == nil && data["EventRef"] == nil &&
		data["BandRef"] == nil && data["CommunityRef"] == nil && r.URL.Query().Get("key") != "original" {
		if n, ok, err := s.store.MySongbookKey(r.Context(), person.ID, variantID); err == nil && ok && n != 0 {
			data["StudentKey"], data["StudentKeySet"] = n, true
			data["SongbookKey"] = s.newSongbookKeyLine(r, s.langOf(w), variant, person.ID, songKeyName(sg), n, true, "")
		}
	}
	if n, ok := data["StudentKey"].(int); ok {
		data["StudentKeyName"] = chordsym.KeyIn(songKeyName(sg), n)
	}
	data["BrandKind"], data["BrandID"] = s.songBrandContext(r, data, variant)
	// A band's place (its song, or opened from its songbook or set list):
	// the reference's words say "the band" — the header's uploaded
	// recording, the Reference view's (play.html); anyone else's are plain.
	if rh, ok := data["RefHead"].(songRefHead); ok {
		rh.Band = data["BrandKind"] == "band"
		data["RefHead"] = rh
	}
	// A coaching community's key per student (store/vocal_slider.go), in
	// the key's panel: that community's students, the coming week's
	// lessons first, when the song was opened in that community. (The
	// voice's slider is no longer a community's option: everyone has a
	// level per part they play, "Je joue", player_parts.go.)
	if coachCtx != 0 {
		if o, err := s.store.CommunityPlayerOptions(r.Context(), coachCtx); err == nil && o.CoachStudentKeys {
			data["CoachStudentKeys"] = coachCtx
			data["SingPanel"] = true
		}
	}
	// A lesson, or a song all in its Guitar Pro file: the lesson view
	// (lessonview.go), its video first.
	lessonViewData(data, sg, s.songLessonFor(r, variantID, data))
	// The key's chip in the dock names the key: "Ton −1 · C#m".
	data["SongKeyName"] = songKeyName(sg)
	// "Je joue": the reader's parts here, a level each (player_parts.go).
	data["PlayerParts"] = s.playerPartsData(r, data, variantID)
	// Where the song's "‹" goes, on the desktop header and the phone's bar.
	back := songBackFor(data, s.langOf(w))
	data["Back"] = back
	// The line above the header: that place again, the song's position
	// in it; else the song's owner. Previous and next keep the place.
	place, _ := data["Place"].(songPlace)
	var owner songOwnerPlace
	if back.Kind == "catalog" {
		owner = s.songOwnerPlace(r, variant, s.langOf(w))
	}
	data["Crumb"] = songCrumbFor(back, place, owner, s.langOf(w))
	data["NavQuery"] = songNavQuery(data)
	// "Accorder": the tuner, in place, the song's own tunings first (the
	// /tuner page's, tuner.go).
	data["SongTunings"] = songTunings(gp.Facts, s.langOf(w))
	data["SongTitle"] = data["Title"]
	data["SongCapo"] = songCapo(sg.Get("capo"))
	data["EditPlaceKey"] = editPlaceKeyOf(data, songKeyName(sg), s.langOf(w))
	s.render(w, "song", data)
}

// editPlaceKey is the place a catalogue song was opened from, when it
// keeps the song in a key of its own: the editor's note says the fix
// leaves that key as it is.
type editPlaceKey struct {
	Place, Key string
}

// editPlaceKeyOf is that, from the page's data (a songbook's, a
// student's or a group's, an event's key), or nil.
func editPlaceKeyOf(data map[string]any, songKey, lang string) *editPlaceKey {
	if data["Catalogue"] != true {
		return nil
	}
	n, _ := data["StudentKey"].(int)
	if set, _ := data["StudentKeySet"].(bool); !set || n == 0 {
		return nil
	}
	k := &editPlaceKey{Key: chordsym.KeyIn(songKey, n)}
	if sk, ok := data["SongbookKey"].(*songbookKeyLine); ok && sk != nil {
		k.Place = sk.Songbook
		if sk.Mine || k.Place == "" {
			k.Place = i18n.T(lang, "your songbook")
		}
		if k.Key == "" {
			k.Key = sk.Label
		}
	} else if c, _ := data["KeyContext"].(string); c != "" {
		k.Place = c
	}
	if k.Place == "" || k.Key == "" {
		return nil
	}
	return k
}

// songCoachingContext is the coaching community a song page was opened in,
// 0 for none: one of its songbooks (a student's, a group's, its
// repertoire), its own page (?community=), or one of its events (a
// program). A band, a set list, or nowhere in particular is none.
func (s *Server) songCoachingContext(r *http.Request, data map[string]any) int64 {
	ctx := r.Context()
	coaching := func(id int64) int64 {
		if c, err := s.store.GetCommunity(ctx, id); err == nil && c.IsCoaching() {
			return id
		}
		return 0
	}
	if id, ok := data["EventRef"].(int64); ok {
		if ev, err := s.store.GetEvent(ctx, id); err == nil && ev.OwnerKind == "community" {
			return coaching(ev.OwnerCommunityID)
		}
		return 0
	}
	if data["SetlistRef"] != nil || data["BandRef"] != nil {
		return 0
	}
	if kind, id := s.songBrandContext(r, data, nil); kind == "community" {
		return coaching(id)
	}
	return 0
}

// bandEditOffer is the band whose version Edit offers on this song page
// (handleSong's BandOffer), or nil: the band the page was opened from
// (?band=, a band's ?setlist= or ?songbook=), when personID is a current
// member of it, it links the song (store.BandsHolding), it may have its
// own version of it (SongRights.CanVersionFor: a public song) and
// personID may make that (store.MayMakeVersion: the band's songbook is
// theirs to change — not a guest's, unless the band lets its guests). With
// no band in the address there's none: a song opened from home, a search
// or one's own songbook isn't a band's (the ⋯ menu offers each band's
// arrangement on The Big Songbook's songs, handleSong's Arrangements).
func (s *Server) bandEditOffer(r *http.Request, personID int64, variant *store.CatalogVariant, rights *store.SongRights) *store.BandHold {
	bandID := s.songPageBand(r)
	if bandID == 0 {
		return nil
	}
	for _, h := range s.bandArrangementHolds(r, personID, variant, rights) {
		if h.BandID == bandID {
			return &h
		}
	}
	return nil
}

// bandArrangementHolds is the bands of personID's that link the song and
// whose arrangement of it personID may make (bandEditOffer's rules), by
// name.
func (s *Server) bandArrangementHolds(r *http.Request, personID int64, variant *store.CatalogVariant, rights *store.SongRights) []store.BandHold {
	holds, err := s.store.BandsHolding(r.Context(), personID, variant.VariantID)
	if err != nil {
		return nil
	}
	var out []store.BandHold
	for _, h := range holds {
		owner := store.BandOwner(h.BandID)
		if !rights.CanVersionFor(owner) {
			continue
		}
		if ok, err := s.store.MayMakeVersion(r.Context(), personID, variant.VariantID, owner, false); err != nil || !ok {
			continue
		}
		out = append(out, h)
	}
	return out
}

// arrangementOffer is one arrangement the viewer may make of The Big
// Songbook's song (handleSong's Arrangements): a band's, a community's or
// their own ("copy"). song.js posts it like Edit's offers did
// (#edit-offer-go's data-action).
type arrangementOffer struct {
	Action      string // "band", "community", "copy"
	BandID      int64
	CommunityID int64
	SongbookID  int64 // the songbook the arrangement takes the link's place in (0: every one of its owner's)
	Name        string
	// OwnSongID: the band has its own arrangement of the song already
	// (it then takes the link's place, nothing new is made).
	OwnSongID int64
	// Coaching: a coaching community's, which its coach and the students
	// it's given to share.
	Coaching bool
}

// arrangementOffers lists the arrangements personID may make of The Big
// Songbook's song: the page's community's (community, from
// communityEditOffer), each band of theirs that links it (the page's band
// first), and their own (copy).
func (s *Server) arrangementOffers(r *http.Request, personID int64, variant *store.CatalogVariant, rights *store.SongRights, community *songCommunityOffer, copy bool) []arrangementOffer {
	var out []arrangementOffer
	if community != nil {
		out = append(out, arrangementOffer{Action: "community", CommunityID: community.CommunityID, SongbookID: community.SongbookID, Name: community.Name, Coaching: community.Coaching})
	}
	pageBand := s.songPageBand(r)
	var songbookID int64
	if id, err := strconv.ParseInt(r.URL.Query().Get("songbook"), 10, 64); err == nil {
		songbookID = id
	}
	var bands []arrangementOffer
	for _, h := range s.bandArrangementHolds(r, personID, variant, rights) {
		o := arrangementOffer{Action: "band", BandID: h.BandID, Name: h.BandName, OwnSongID: h.OwnSongID}
		if h.BandID == pageBand {
			o.SongbookID = songbookID
			bands = append([]arrangementOffer{o}, bands...)
			continue
		}
		bands = append(bands, o)
	}
	if community != nil || pageBand == 0 {
		out = append(out, bands...)
	} else {
		out = append(bands, out...)
	}
	// Their own, but where a coaching community links it: the coach and
	// its students share the community's (docs/coaching.md).
	if copy && (community == nil || !community.Coaching) {
		out = append(out, arrangementOffer{Action: "copy"})
	}
	return out
}

// placeBandArrangement is the arrangement the band the page was opened
// from (?band=, its set list or songbook) has of songID, when personID is
// a current member of it: the shared song's page says so, with a link to
// it (nil when none).
func (s *Server) placeBandArrangement(r *http.Request, personID, songID int64) *store.BandVersion {
	bandID := s.songPageBand(r)
	if bandID == 0 {
		return nil
	}
	v, err := s.store.BandArrangementOf(r.Context(), personID, bandID, songID)
	if err != nil {
		return nil
	}
	return v
}

// editOfferKind is what Edit offers instead of the editor (play.html's
// #edit-offer-dialog, the editor's data-edit-offer): "band", "community",
// "copy", or "" when Edit opens the editor or there's no Edit.
func editOfferKind(canEdit, canEditChart, canCopy bool, band *store.BandHold, community *songCommunityOffer) string {
	switch {
	case canEdit || canEditChart:
		return ""
	case band != nil:
		return "band"
	case community != nil:
		return "community"
	case canCopy:
		return "copy"
	}
	return ""
}

// songCommunityOffer is the community whose version Edit offers on a song
// its songbook links (handleSong's CommunityOffer).
type songCommunityOffer struct {
	CommunityID int64
	Name        string
	SongbookID  int64
	// Coaching: a coaching community's, whose version every songbook of
	// its takes (the repertoire, each student's and group's), the coach
	// and the students and groups it's given to editing it
	// (docs/coaching.md).
	Coaching bool
}

// communityEditOffer is that offer, or nil: the page was opened from a
// community's songbook (?songbook=) that links the song (not the
// community's own), personID may make the community's version
// (store.MayMakeVersion: who edits its repertoire; in a coaching
// community, the coach, and a student the song is given to), and the
// community may (a public song). Opened from elsewhere, a song a
// coaching community links that personID can't edit in place offers that
// community's version when they may make it (coachingEditOffer).
func (s *Server) communityEditOffer(r *http.Request, personID int64, variant *store.CatalogVariant, rights *store.SongRights) *songCommunityOffer {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.URL.Query().Get("songbook"), 10, 64)
	if err != nil || id <= 0 {
		if rights.Edit {
			return nil
		}
		return s.coachingEditOffer(r, personID, variant, rights)
	}
	sb, err := s.store.GetSongbook(ctx, id)
	if err != nil || sb.OwnerKind != "community" {
		return nil
	}
	owner := sb.Owner()
	if rights.Owner == owner || !rights.CanVersionFor(owner) {
		return nil
	}
	if _, in, err := s.store.SongbookEntryKey(ctx, sb.ID, variant.VariantID); err != nil || !in {
		return nil
	}
	if ok, err := s.store.MayMakeVersion(ctx, personID, variant.VariantID, owner, false); err != nil || !ok {
		return nil
	}
	c, err := s.store.GetCommunity(ctx, sb.OwnerCommunityID)
	if err != nil {
		return nil
	}
	return &songCommunityOffer{CommunityID: c.ID, Name: c.Name, SongbookID: sb.ID, Coaching: c.IsCoaching()}
}

// coachingEditOffer is the community's version offered on a song opened
// from no songbook (search, home, a link): the first coaching community
// whose songbooks link it and whose version personID may make — its
// coach, or a student it's given to.
func (s *Server) coachingEditOffer(r *http.Request, personID int64, variant *store.CatalogVariant, rights *store.SongRights) *songCommunityOffer {
	ctx := r.Context()
	communities, err := s.store.CoachingCommunitiesOfSong(ctx, variant.VariantID)
	if err != nil {
		return nil
	}
	for _, c := range communities {
		owner := store.CommunityOwner(c.ID)
		if rights.Owner == owner || !rights.CanVersionFor(owner) {
			continue
		}
		if ok, err := s.store.MayMakeVersion(ctx, personID, variant.VariantID, owner, false); err != nil || !ok {
			continue
		}
		return &songCommunityOffer{CommunityID: c.ID, Name: c.Name, Coaching: true}
	}
	return nil
}

func variantVersionName(v *store.CatalogVariant) string {
	if v == nil {
		return ""
	}
	return v.VersionName
}

// songPageBand is the band a song page was opened from: ?band=N, a band's
// set list (?setlist=N) or a band's songbook (?songbook=N); 0 for none.
func (s *Server) songPageBand(r *http.Request) int64 {
	q := r.URL.Query()
	if id, err := strconv.ParseInt(q.Get("band"), 10, 64); err == nil && id > 0 {
		return id
	}
	if id, err := strconv.ParseInt(q.Get("setlist"), 10, 64); err == nil && id > 0 {
		if meta, err := s.store.GetSetlistMeta(r.Context(), id); err == nil && meta.Scope == "band" && meta.OwnerBandID != nil {
			return *meta.OwnerBandID
		}
		return 0
	}
	if id, err := strconv.ParseInt(q.Get("songbook"), 10, 64); err == nil && id > 0 {
		if sb, err := s.store.GetSongbook(r.Context(), id); err == nil && sb.OwnerKind == "band" {
			return sb.OwnerBandID
		}
	}
	return 0
}

// songBasedOn is the song a band's song was made from (play.html's
// "Based on" line): its title and artist, and whose it is (Kind is its
// visibility, Owner its band's, community's or person's name).
type songBasedOn struct {
	VariantID     int64
	Title, Artist string
	Kind, Owner   string
}

func variantVisibility(v *store.CatalogVariant) string {
	if v == nil {
		return ""
	}
	return v.Visibility
}

// songBrandContext is whose look a song page takes (internal/brand): the
// band or community it was opened from — directly, or through one of
// their songbooks or setlists — else the band or community that owns the
// song; "" for neither.
func (s *Server) songBrandContext(r *http.Request, data map[string]any, variant *store.CatalogVariant) (string, int64) {
	if id, ok := data["BandRef"].(int64); ok {
		return "band", id
	}
	if id, ok := data["CommunityRef"].(int64); ok {
		return "community", id
	}
	if id, ok := data["SongbookRef"].(int64); ok {
		if sb, err := s.store.GetSongbook(r.Context(), id); err == nil {
			switch {
			case sb.OwnerKind == "band" && sb.OwnerBandID != 0:
				return "band", sb.OwnerBandID
			case sb.OwnerKind == "community" && sb.OwnerCommunityID != 0:
				return "community", sb.OwnerCommunityID
			}
		}
	}
	if ref, ok := data["SetlistRef"].(string); ok {
		if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
			if meta, err := s.store.GetSetlistMeta(r.Context(), id); err == nil {
				if kind, owner := setlistOwner(meta); kind != "" {
					return kind, owner
				}
			}
		}
	}
	if variant != nil {
		switch {
		case variant.OwnerBandID != nil:
			return "band", *variant.OwnerBandID
		case variant.OwnerCommunityID != nil && variant.Visibility != "global":
			// The Big Songbook's (the catalogue's) has no look of its own.
			return "community", *variant.OwnerCommunityID
		}
	}
	return "", 0
}

// setlistOwner is the band or community a setlist belongs to, if any.
func setlistOwner(meta *store.SetlistMeta) (string, int64) {
	switch {
	case meta.OwnerBandID != nil:
		return "band", *meta.OwnerBandID
	case meta.OwnerCommunityID != nil:
		return "community", *meta.OwnerCommunityID
	}
	return "", 0
}

// songNeighbor is the previous or next song's card in the header: its
// title, without loading the whole chart (the tempo and key stay for the
// cards' other readers).
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

// neighborsAt loads the songs either side of a place in a list.
func (s *Server) neighborsAt(at songPlace) (prev, next *songNeighbor, _ songPlace) {
	if at.PrevID != 0 {
		prev = s.loadSongNeighbor(at.PrevID)
	}
	if at.NextID != 0 {
		next = s.loadSongNeighbor(at.NextID)
	}
	return prev, next, at
}

// songbookNeighbors returns the entries immediately before/after
// variantID in songbookID's own order, and its position there, so every
// song page offers SOME way to flip to the next one, not only when it was
// opened via a setlist.
func (s *Server) songbookNeighbors(ctx context.Context, songbookID, variantID int64) (prev, next *songNeighbor, at songPlace) {
	groups, err := s.store.ListSongsOrdered(ctx, songbookID)
	if err != nil {
		return nil, nil, songPlace{}
	}
	var flat []int64
	for _, g := range groups {
		flat = append(flat, g.VariantIDs...)
	}
	return s.neighborsAt(listPlace(flat, variantID))
}

// setlistNeighbors flattens every set's entries into one running order (a
// setlist is a single gig, sets back to back) and returns the entries
// immediately before/after variantID in it, and its position.
func (s *Server) setlistNeighbors(ctx context.Context, setlistRef string, variantID int64) (prev, next *songNeighbor, at songPlace) {
	id, err := strconv.ParseInt(setlistRef, 10, 64)
	if err != nil {
		return nil, nil, songPlace{}
	}
	l, _, _, err := s.store.LoadSetlistByID(ctx, id)
	if err != nil {
		return nil, nil, songPlace{}
	}
	var flat []int64
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			if e.IsVariant() {
				flat = append(flat, e.VariantID)
			}
		}
	}
	return s.neighborsAt(listPlace(flat, variantID))
}

// songOwnerPlace is the page of the song's owner, for the line above the
// header of a song opened from nowhere in particular: its band's
// repertoire, its community's songbook, or the reader's own songbook;
// none for a catalogue song, or a band's or community's the reader isn't
// in.
func (s *Server) songOwnerPlace(r *http.Request, v *store.CatalogVariant, lang string) songOwnerPlace {
	ctx := r.Context()
	person := personFromContext(r)
	if v == nil || person == nil {
		return songOwnerPlace{}
	}
	switch {
	case v.OwnerBandID != nil:
		if ok, _ := s.store.IsCurrentBandMember(ctx, *v.OwnerBandID, person.ID); !ok && !person.IsAdmin {
			return songOwnerPlace{}
		}
		if b, err := s.store.GetBand(ctx, *v.OwnerBandID); err == nil {
			return songOwnerPlace{Href: fmt.Sprintf("/bands/%d/catalog", b.ID), Name: b.Name}
		}
	case v.OwnerCommunityID != nil && v.Visibility != "global":
		if ok, _ := s.store.IsCurrentCommunityMember(ctx, *v.OwnerCommunityID, person.ID); !ok && !person.IsAdmin {
			return songOwnerPlace{}
		}
		c, err := s.store.GetCommunity(ctx, *v.OwnerCommunityID)
		if err != nil {
			return songOwnerPlace{}
		}
		o := songOwnerPlace{Href: fmt.Sprintf("/communities/%d/songbooks/default", c.ID), Name: c.Name}
		if books, err := s.store.ListSongbooks(ctx, "community", c.ID); err == nil {
			for _, sb := range books {
				if sb.IsDefault && sb.Name != "" {
					o.Name = sb.Name
				}
			}
		}
		return o
	case v.Visibility == "user" && v.OwnerID != nil && *v.OwnerID == person.ID:
		o := songOwnerPlace{Href: "/songbooks/mine", Name: i18n.T(lang, "My songbook")}
		if books, err := s.store.ListSongbooks(ctx, "person", person.ID); err == nil {
			for _, sb := range books {
				if sb.IsDefault {
					o.Href = fmt.Sprintf("/songbooks/%d", sb.ID)
				}
			}
		}
		return o
	}
	return songOwnerPlace{}
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
		Text       string    `json:"text"`
		Song       *jsonSong `json:"song"`
		Instrument string    `json:"instrument"`
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
	// Score voices are plain text in the body now (song.Section.Scores —
	// see that field's own doc comment), so the live preview can resolve
	// them the same way the real Play page does — a section's
	// "score[label]:" block shows its real notation here too, not just
	// after a round trip to Play. Same tab.LoadScores/attachTabRiffs/
	// buildDrumMap call handleSong itself makes.
	num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
	riffs, scoreErrs := tab.LoadScores(sg)
	attachTabRiffs(sections, riffs, atoiOr(num, 4), atoiOr(den, 4), sg.Get("swing"), req.Instrument)
	drumMap := buildDrumMap(sections, riffs, atoiOr(orDefault(sg.Get("tempo"), "120"), 120), 0)
	for _, e := range scoreErrs {
		log.Printf("preview: %v", e)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webassets.TemplatesFor(s.langOf(w)).ExecuteTemplate(w, "sheet-body", map[string]any{
		"Sections":  sections,
		"MapLines":  buildMapLines(sections),
		"Jazz":      buildJazzChart(sg, sections),
		"DrumMap":   drumMap,
		"VariantID": int64(0), // unsaved preview — no real variant to re-fetch drummer-rows against; alphatab-tab.js skips the re-fetch when this is absent/zero
	}); err != nil {
		log.Printf("render preview: %v", err)
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	s.addMembersTab(w, data)
	s.addUnreadCount(w, data)
	s.addNav(w, name, data)
	s.addSpaceNav(w, name, data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webassets.TemplatesFor(s.langOf(w)).ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

// songStartsWithPickup reports whether sg.Sections[si] — the song's first
// section with a grid — opens with a pickup bar (tab.StartsWithPickup, which
// the PDF asks too).
func songStartsWithPickup(sg *song.Song, si int) bool { return tab.StartsWithPickup(sg, si) }

// isSilentBar: a grid bar that names no chord at all — "?" (unknown), "."
// (nothing new), "R" (rest) or N.C.
func isSilentBar(tokens []string) bool { return tab.SilentBar(tokens) }
