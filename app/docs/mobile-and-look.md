# Phone experience, branding, and look & feel

Status: first round **built** in PR #8, the phone plan of §1 in the
`mobile-phase2` branch (marked [x]); §2's next steps and §3 are the plan.

## 1. Phone review (375 × 812, every main page)

### Navigation

- [x] The **More** sheet repeated the four tab bar destinations. It now
  lists only what the tab bar doesn't have (Communities, Metronome,
  Profile, Admin), except on the song page, whose own bar replaces the
  tab bar there.
- [x] **Communities were buried in More.** The fourth tab is now
  **Groups** (`/groups`): the gigs coming up, then the person's bands and
  communities, as the same cards Home uses. It stays lit on a band's,
  a community's, a set list's and an event's pages.
- [x] **Songbooks and Bands overlapped.** With Groups in the tab bar,
  Songbooks is the music and Groups the people and dates. The full Bands
  and Communities lists are in More, with Set lists.
- [x] **Set lists had no entry point** on a phone. The next gig now opens
  Home (and Groups); set lists are in More; the Set lists page shows each
  list as its title (the running order), Practice / Listen / Click track,
  and its exports folded under *Export*, with *New set list* folded too.

### Page headers

- [x] Headers showed the app logo, the page title, a link and the bell on
  one line. The logo link is gone on phones (the Home tab does that).
- [x] One header pattern on phones: back arrow · title on one line · the
  page's one action and the bell, applied by CSS to every page's header
  (the back link keeps its text for screen readers). Header links to tab
  bar destinations are dropped. The shared layout partial (37 copies of
  the head and header) is still §3.1.

### Lists

- [x] Song titles in a songbook were cut to "Les secrets…" by the tempo
  and badges. Titles now wrap; tempo and badges go underneath.
- [x] "My bands / All bands" ran together as plain text; it's a pill
  toggle now.
- [x] A band's seven tabs wrapped onto two rows; they scroll on one.
- [x] A band's repertoire repeated the band's own name as the artist of
  every original.
- [x] Band cards said "0 changes since your last visit"; the line only
  shows when there are changes. Home, My bands, My communities and Groups
  share one card partial (`cards.html`). Home shows *Coming up* first when
  there's a gig, and an empty *Needs your attention* is one line.
- [x] The Library opens on one search field; artist, genre and sort are
  folded under *Filters* (open when one is in use).
- [x] Song badges are drawn as bare icons on phones, so they no longer
  look like buttons.
- [x] The members page: pending people and the quick-add form stack
  instead of squeezing six columns into a line; internal `pending-…`
  usernames are no longer shown.

### Song page (the one that matters most on stage)

- [x] A saved *Drummer* view opened every song as empty staves when the
  song had no drum part; such songs now open as a chart.
- [x] The tab/staff picker is labelled ("Guitar as …") and personal.
- [x] The structure strip keeps section names and bar counts on phones,
  in larger type and tap-sized; the chord list is dropped.
- [x] *View* holds how the song is drawn: the views, the **Part**
  (Chords / each instrument — it had no phone entry at all before), the
  tab/staff choice, and Stage mode. *Song* holds what you do with it:
  Edit, Notes, Reference, exports, versions, history.
- [x] **Stage mode** (`stage.js`), from the View sheet or the desktop
  sidebar: larger type, no sidebar/bars/player/pickers, the screen kept
  awake (Wake Lock), a swipe or the pedal keys for the next or previous
  song, their names at the bottom, a close button and Escape. It stays on
  from song to song in that tab.

### Editing on a phone

- [x] The score editor has a keypad and two bars per row (previous PR).
- [x] The editor's header is one row on phones (title, undo, redo, Save,
  close; Discard only when there is something to discard) with the five
  tabs scrolling on one line under it. "Hover a section…" is hidden on
  touch screens.
- [x] Content pass: the members, repertoire, proposals, set lists,
  events and communities hints are one plain sentence each; no more
  "variant", "band-visibility" or role jargon on those pages.

### Theme

- [x] The light/dark choice applies on every page: it's set on `<html>`
  before the page draws, and *Appearance* (Auto / Light / Dark) is in the
  More sheet. It stays a per-device choice (the same localStorage key as
  the song page's button), since a phone on stage and a laptop at home
  often want different ones.

### Sections, home and the calendar (built)

- [x] **Band sections on a phone.** Seven tabs scrolled out of sight at 375px.
  On a phone the band's overview now lists its sections, each with where it
  stands (30 songs · 2 to vote on · next rehearsal Wednesday), grouped as
  playing / deciding / running the band; section pages drop the tab row and
  their header leads back. Desktop keeps the tabs (one shared partial,
  `band_nav.html`). Rule for the rest of the app: tabs when they fit without
  scrolling (community, set list, profile), a list to tap into when they don't.
- [x] **Settings split from Members.** Name, what the band plays, its look and
  deleting it moved to `/bands/{id}/settings`; the music-type select left the
  cramped header.
- [x] **Rehearsals.** A band's Calendar (was Events) plans a rehearsal: date,
  times, place, notes and a set list to practise (a new empty one named after
  the date, or an existing one). The rehearsal page shows that set list with
  Practice; band events no longer show the community registration blocks.
- [x] **Set lists can take songs.** The running-order page had no way to add
  songs; it now has a picker (the band's repertoire, or a library search).
- [x] **Home answers "what now?"** Next up (the next gig or rehearsal, with
  Practice), Needs you (hidden when empty), the month's calendar (gigs from set
  lists, band and community events, rehearsals), Continue (the last songs
  opened, server-side in `song_visits`, so it follows you between devices),
  New in your bands, and one row of links to your groups. The full band,
  community and songbook cards stay on their own tabs.
- [x] **Time zone.** Times are shown in `TZ` (Europe/Paris by default in the
  deploy files); they were shown in UTC.

## 2. Branding (built)

A community or a band has a **look**: colors, a title font and a text
font from a short list, the button shape (square, rounded, pills), a mark
or logo, a tagline and a website. Its pages, its songbooks and setlists,
and the songs opened from them are drawn in that look: the brand
overrides the app's design tokens on `<body>`, so no layout changes.

- **Who sets it:** a community's admins (Admin tab, *Look & feel*); a
  band's core members (Settings tab).
- **Presets:**
  - *Blues & Rock*, after florentpassamonti.fr: black header, white
    pages, Assistant, square black buttons, the red of his covers.
  - *Night*, after otherlies.fr: ink blue, ice blue and rose, Fraunces
    titles over Space Grotesk, pill buttons, the crescent mark.
  - *Camp*, after bands-camp.com (Bands Camp, its Futuradio stream and
    the Châtel All Stars course share it): grey header, black Anton
    titles over Inter, hot pink, square buttons, the "since 2012" stamp.
  - *Jam*, after app.jamspace.co: white pages, teal and slate, Bebas
    Neue titles over Nunito Sans, soft corners.
  - *Stage* and *Paper* as starting points for others.
- **Safety:** only hex colors, fonts from the list (loaded from Google
  Fonts), https links, short texts (`internal/brand.Clean`).

**Next for branding:**
1. Upload a logo and a banner photo (stored like the Guitar Pro files)
   instead of linking one.
2. Warn in the form when text and background don't contrast enough
   (WCAG 4.5:1), since a brand is typed in by a band member.
3. Brand the PDF exports and the emails a community sends.
4. A community's own entry page for newcomers (its banner, its
   songbooks, how to join), shareable as a link.
5. Optionally, a chart font per brand for chord names.

## 3. Look & feel, overall

1. **One layout.** A shared head and page header partial instead of 37
   copies: consistent headers, and theming and branding applied in one
   place.
2. **A small design system.** The color tokens are already there; add a
   type scale (page title, section, body, small), spacing steps actually
   used, and one radius per kind of control. Write it down as a
   `/dev/styleguide` page showing every component: page header, section
   header, card, list row, pill tabs, buttons, chips, empty state, sheet.
3. **A display font for the app itself.** The marketing pages use
   Fraunces and Inter, the app uses the system font with EB Garamond in
   charts. Using `--font-display` for page and section titles app-wide
   (Fraunces) would make the app and its site one product.
4. **Calmer lists.** Striped rows, borders and tinted cards all at once
   read busy on a phone: one row style (hairline separators), with color
   kept for what's actionable or current.
5. **Empty states** that say what to do next, with one button.
6. **Dark mode everywhere**, and a *stage* palette for the song page.
7. **Motion:** sheets and drawers slide (they mostly snap today); the
   current bar highlight eases.
8. **Accessibility pass:** tap targets, contrast (including brands),
   focus outlines on every control.
