# Phone experience, branding, and look & feel

Status: first round **built** on branch `mobile-branding` (marked [x]); the
rest is the plan.

## 1. Phone review (375 × 812, every main page)

### Navigation

- [x] The **More** sheet repeated the four tab bar destinations. It now
  lists only what the tab bar doesn't have (Communities, Metronome,
  Profile, Admin), except on the song page, whose own bar replaces the
  tab bar there.
- [ ] **Communities are buried in More.** For someone whose music lives in
  a community (Florent's students), it's their main space. The fourth tab
  should be *Bands* or *Communities* depending on what the person belongs
  to, or one *Groups* tab listing both.
- [ ] **Songbooks and Bands overlap.** Songbooks lists "Band repertoires",
  Bands lists the same bands with their songbook. One "My music" hub
  (my songbook, my bands' and communities' songbooks, recent songs) would
  replace both tabs and free a slot for Communities or Setlists.
- [ ] **Setlists have no entry point** on a phone except through a band.
  A band's next gig is the most time-critical thing on a phone.

### Page headers

- [x] Headers showed the app logo, the page title, a link and the bell on
  one line. The logo link is gone on phones (the Home tab does that).
- [ ] One header pattern everywhere: back · title · one action. Today
  each template writes its own (37 copies of the head and header).

### Lists

- [x] Song titles in a songbook were cut to "Les secrets…" by the tempo
  and badges. Titles now wrap; tempo and badges go underneath.
- [x] "My bands / All bands" ran together as plain text; it's a pill
  toggle now.
- [x] A band's seven tabs wrapped onto two rows; they scroll on one.
- [x] A band's repertoire repeated the band's own name as the artist of
  every original.
- [ ] Band cards on Home say "0 changes since your last visit": hide when
  there's nothing.
- [ ] The Library opens on a four-field filter form that fills the first
  screen. A search field with a *Filters* sheet would put songs first.
- [ ] Badge icons and chips are under the 44 px tap size and mostly
  decorative on a phone.

### Song page (the one that matters most on stage)

- [x] A saved *Drummer* view opened every song as empty staves when the
  song had no drum part; such songs now open as a chart.
- [x] The tab/staff picker is labelled ("Guitar as …") and personal.
- [ ] The structure strip's chord list is 8 px text: on a phone, keep the
  section names and bar counts, drop the chords.
- [ ] The *View* sheet mixes views (Song, Jazz chart…) and actions (Edit,
  Notes); the instrument picker (Chords / Guitar / Bass) isn't in it.
  Split it: *View* (views + instrument + tab/staff), *Song* (edit,
  notes, versions, export).
- [ ] A **stage mode**: larger type, no chrome, screen kept awake, swipe
  to the next song of the setlist.

### Editing on a phone

- [x] The score editor has a keypad and two bars per row (previous PR).
- [ ] The editor's own header (title, undo, save, close, five tabs) takes
  a third of the screen; on a phone it should fold into one row with a
  tab menu.
- [ ] Hints written for developers ("No owner/admin role here — any
  current member…", internal names) are still on several pages; a
  content pass should keep one short sentence where one is needed.

### Theme

- [ ] The light/dark choice is only applied on the song page; every other
  page ignores it. It should be a profile setting applied everywhere.

## 2. Branding (built)

A community or a band has a **look**: colors, a title font and a text
font from a short list, the button shape (square, rounded, pills), a mark
or logo, a tagline and a website. Its pages, its songbooks and setlists,
and the songs opened from them are drawn in that look: the brand
overrides the app's design tokens on `<body>`, so no layout changes.

- **Who sets it:** a community's admins (Admin tab, *Look & feel*); a
  band's core members (Members tab).
- **Presets:**
  - *Blues & Rock*, after florentpassamonti.fr: black header, white
    pages, Assistant, square black buttons, the red of his covers.
  - *Night*, after otherlies.fr: ink blue, ice blue and rose, Fraunces
    titles over Space Grotesk, pill buttons, the crescent mark.
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
