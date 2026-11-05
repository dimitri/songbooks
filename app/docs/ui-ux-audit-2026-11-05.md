# Songbooks UI/UX Audit — November 2026

## Executive summary

Songbooks is further along than "patchwork" suggests. There is a real token system (`:root` custom properties, a light/dark pair, a deliberate dark sidebar), a documented mobile-first tab-bar architecture, and — unusually for an incrementally-built app — extensive in-code design rationale left as CSS/template comments explaining *why* a rule exists. The craftsmanship is genuinely good in places (the jazz-chart rendering, the admin "table-stack" mobile pattern, the getting-started wizard).

The problem is coverage, not vision: good patterns exist, but they were applied where a specific bug was reported, not swept across every surface that has the same shape. The clearest example is the mobile table fix itself — a well-designed `.table-stack` component exists and is wired into `admin.html`'s five tables, but the structurally identical `school_admin.html` (five more tables, same density, same audience) still ships the pre-fix plain `<table>` that is "unusable on mobile," in the CSS's own words, one file over. The same "fixed once, not generalized" pattern repeats in color tokens (one component references `--line`, `--card`, `--ink` — variables that don't exist in `:root`, with hardcoded fallbacks, clearly carried over from a different token vocabulary), in touch-target sizing (icon buttons and the primary button are both under the WCAG/Apple/Material minimums), and in focus-visible coverage (two interactive elements explicitly strip the native focus ring and don't fully replace it).

None of this requires a rebuild. It requires a sweep: finish applying the patterns that already exist in the codebase to the surfaces that were missed when they were introduced, tighten the token system so nothing can silently fall outside it, and fix a short list of genuine accessibility failures (sub-AA contrast on two status-color pairs, sub-minimum touch targets, two unreplaced focus rings). The "steps to compliance" section at the end is written as a mechanical punch list for exactly that follow-up pass.

## Methodology

**Codebase (read-only):**
- `internal/webassets/static/css/app.css` in full (3,817 lines) — the entire hand-rolled stylesheet, no framework.
- `internal/webassets/templates/icons.html` (shared nav/icon partials: `desktop-sidebar`, `global-tabbar`, `site-nav-fullpage`) in full.
- Sampled broadly across the other 48 templates: `admin.html`, `admin_person.html`, `school_admin.html`, `band_members.html`, `band.html`, `school.html`, `catalog.html`, `songbooks_index.html`, `event.html`, `community_events.html`, `invite.html`, `login.html`, `play.html`, `project.html`, `profile.html`, `jams_index.html`, `notifications.html`, `variant_history.html`, and others, plus targeted greps across all 49 for inline styles, table markup, button classes, `confirm()` dialogs, and empty-state markup.
- Skimmed `static/js/*.js` (editor.js, song.js, project.js, audio-player.js, etc.) for modal/confirmation/interaction patterns.
- Reviewed `git log --oneline` (362 commits) and filtered for `mobile`, `responsive`, `table`, and `design` to separate what has already been iterated on from what hasn't.
- Checked `docs/*.md` (three product-feature docs; no prior design/UX doc existed before this one).
- Computed WCAG relative-luminance contrast ratios directly from the `:root` hex values for every semantic color pair (ok/warn/danger/current text-on-background, muted/faint text-on-background, sidebar) using the standard WCAG formula.

**External research** (search-engine results against primary sources where available, secondary summaries flagged as such below):
- Nielsen Norman Group — consistency & standards heuristic, hostile error messages / form-validation timing.
- W3C WCAG 2.2 — SC 1.4.3 Contrast (Minimum), SC 2.5.8 Target Size (Minimum), SC 2.4.7 Focus Visible.
- Apple Human Interface Guidelines and Material Design — touch-target baselines (44pt / 48dp), cited as cross-platform baselines even though this is a web app.
- Laws of UX (Fitts's Law, Hick's Law, Jakob's Law).
- Refactoring UI / spacing-scale literature, and web.dev-adjacent guidance on content-driven responsive breakpoints.

No application code was changed. This report is diagnostic only.

---

## Findings by theme

<style>
.sev{display:inline-block;padding:2px 9px;border-radius:4px;font-size:0.8em;font-weight:700;color:#fff;}
.sev-high{background:#a52f2f;}
.sev-med{background:#b8762a;}
.sev-low{background:#5f554a;}
</style>

### 1. Design tokens & visual consistency

**Principle.** A design system's entire value is that every component draws from the *same* finite palette and spacing scale, so a reader's eye calibrates once and reuses that calibration everywhere (Refactoring UI's spacing/color-scale argument: a limited, deliberately-spaced set of values, not arbitrary per-commit choices). Nielsen's consistency heuristic makes the same point at the UI level: internal consistency (the same thing means the same thing everywhere in *this* product) is what keeps users from having to re-learn each screen (NN/g, "[Consistency and Standards](https://www.nngroup.com/articles/consistency-and-standards/)").

**What's actually there.** The token system is real and reasonably disciplined: `app.css:44–83` defines a full `:root` palette (`--bg`, `--fg`, `--accent`, `--danger`, `--ok`, `--warn`, `--current-*`, `--sidebar-*`), mirrored for `prefers-color-scheme: dark` (`app.css:85–109`) and for an explicit `body[data-theme]` override (`app.css:111–122`). That's more rigor than most hand-rolled apps get to.

**Findings:**

<table>
<tr><td><span class="sev sev-high">HIGH</span></td><td>

`app.css:3807–3808` — `.pdf-mode-select` and `.pdf-notes-label` reference `var(--line, #ccc)`, `var(--card, #fff)`, `var(--ink, #222)`, and `var(--ink-soft, var(--text-muted, #666))`. None of `--line`, `--card`, `--ink`, `--ink-soft`, or `--text-muted` are defined anywhere in `:root` (confirmed by grep across the whole file). This component was evidently ported from a different project or an earlier, abandoned token naming scheme (`--ink`/`--card`/`--line` vs. this app's actual `--fg`/`--bg-raised`/`--border`) and never reconciled. Today it silently works because every fallback happens to roughly match the light theme — but it is **invisible to dark mode**: in dark mode this one control stays light-on-light-gray (`#fff` background, `#222` text) while every surrounding element switches. This is exactly the kind of drift a token system exists to prevent.

</td></tr>
<tr><td><span class="sev sev-med">MEDIUM</span></td><td>

Hardcoded colors still appear outside the token block in several places that *should* be tokenized: `app.css:978` (`background: #c0392b` — a red that doesn't match `--danger: #a52f2f`/`#d9584f`, a second, slightly different "danger red" in the system), `app.css:3770` (`.qr-modal-img { background: #fff; }` — breaks in dark mode, a white box floating in a dark modal), `app.css:168`/`479` (`color: #fff` instead of `var(--accent-contrast)`, which exists for exactly this purpose and is already used correctly elsewhere, e.g. `app.css:3667`).

</td></tr>
<tr><td><span class="sev sev-med">MEDIUM</span></td><td>

115 inline `style="..."` attributes across 34 of the 49 templates (counted via `grep -rn 'style="'`). Most are narrow and defensible — per-row dynamic values like `style="--slots: {{len .Beats}}"` in `play.html:34` and `style="--cap: {{.Capacity}}"` in `play.html:172` are legitimate uses of inline styles to pass server-computed data into CSS custom properties, not a system smell. But a meaningful subset are plain hardcoded layout values that belong in a class: `admin.html:139` and `admin_person.html:68` both hardcode `style="max-width:420px"` / `max-width:480px"` on `.card` instead of a `.card--narrow` variant; `admin_person.html:49` hardcodes `style="min-width:9rem"` on a `<strong>`; `play.html:593,599,600,603` mix five different one-off inline rules (`margin:0`, `padding:.75em 0`, `margin:.25em 0`, `width:100%`) into what is otherwise a well-componentized page. Each one is small, but collectively they mean the stylesheet is not the single source of truth for spacing on these pages — a future global spacing-scale change won't reach them.

</td></tr>
<tr><td><span class="sev sev-low">LOW</span></td><td>

Button class vocabulary has drifted into combinatorial soup: a single-purpose grep for `class="..."` containing `btn` across templates turns up `remove-member-btn danger`, `vote-btn danger`, `remove-entry-btn danger`, `accept-btn primary`, `primary icon-btn`, `primary new-band-setlist-btn`, `membership-remove-btn danger`, alongside the generic `icon-btn`, `link-btn`, `ghost-btn`. `danger` and `primary` are reused as *modifier* classes (good — that's the right pattern, matching `button.primary`/`button.danger` in `app.css:157–168`), but each feature area also invented its own base class (`vote-btn`, `accept-btn`, `fork-btn`, `jam-pick-btn`, `copy-link-btn`, `qr-link-btn`...) rather than composing from a shared small set (`.btn`, `.btn-sm`, `.btn-icon`). The result: 25+ distinct button base classes for what's functionally ~6 button shapes.

</td></tr>
</table>

**Remediation.** Reconcile `--line`/`--card`/`--ink`/`--text-muted` to the real tokens (`--border`/`--bg-raised`/`--fg`/`--fg-muted`) in the two rules that use them; replace the three stray hardcoded hex colors with their token equivalents; introduce 2–3 size/shape modifier classes (`.card--narrow`, a `min-width` utility) to absorb the worst inline-style offenders; consolidate the 25+ one-off button classes onto a documented `.btn` family with modifiers.

---

### 2. Responsive layout & touch targets

**Principle.** WCAG 2.2 SC 2.5.8 Target Size (Minimum, Level AA) requires interactive targets be at least 24×24 CSS px (W3C, "[Target Size (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html)"), but the two platform baselines this app's users actually carry expectations from are stricter: Apple HIG's 44×44pt minimum for iOS/iPadOS controls, and Material Design's 48×48dp minimum for Android (Google, touch-target accessibility guidance). For this specific app, the stakes are higher than average: the stated user is a musician glancing at a phone screen on a stand or stage, often one-handed, often in low light, often mid-song — exactly the "hurried, imprecise tap" scenario Fitts's Law describes, where smaller targets and greater distance both increase error rate (lawsofux.com, Fitts's Law).

**Findings:**

<table>
<tr><td><span class="sev sev-high">HIGH</span></td><td>

`app.css:169` — `button.icon-btn { width: 2.3rem; height: 2.3rem; ... }`. At the default 16px root, that's **36.8×36.8px** — below both the 44pt Apple baseline and the 48dp Material baseline, and it's the single most-used interactive control in the app (18 distinct `icon-btn` call sites found across templates, plus the close/more/edit/metronome icons in the shared nav partials in `icons.html`). This is the exact control a musician taps mid-song on Play, mid-scroll in a setlist, or while standing at a stand.

</td></tr>
<tr><td><span class="sev sev-high">HIGH</span></td><td>

`app.css:151–153` — the base `button` rule (`font-size: 0.9rem; padding: 0.45rem 0.8rem`) computes to roughly **36–37px tall** with default line-height, below the same baselines. Since `button.primary` and `button.danger` (`app.css:157,167`) only override color/weight, not sizing, every primary CTA in the app — "Save", "Accept", "Create band", the confirm button inside every destructive-action dialog — inherits this undersized hit area.

</td></tr>
<tr><td><span class="sev sev-high">HIGH</span></td><td>

The mobile-table fix was never generalized. `app.css:284–320` and its own code comment document exactly the problem: a dense admin table is "unusable on mobile even with `.table-scroll`'s horizontal-scroll fallback," so a `.table-stack` class was built that turns rows into stacked, labeled cards below 600px. It's wired into `admin.html`'s five tables (People, Communities, Pending accounts, Invite links, Single-use invites — confirmed via `grep -n table-stack`). It is **not** applied to the structurally identical community-admin surface, `school_admin.html` (five tables, same density, same column shapes, same `data-label` technique would apply directly), nor to `band_members.html`, `band_proposal.html`, `band_proposals.html`, `jam_songs.html`, `jams_index.html`, `jam_add_songs.html`, `notifications.html`, `variant_history.html`, `schools.html`, or `school.html` — eleven more templates using a bare `<table>`. Several of these (school_admin's member/invite tables especially) are the exact "4+ column, actions-heavy" shape the `.table-stack` comment calls out as needing it, not the shape where horizontal scroll is an acceptable tradeoff.

</td></tr>
<tr><td><span class="sev sev-med">MEDIUM</span></td><td>

16 total `@media` rules in the stylesheet, at seven different breakpoints: 480, 560, 600, 640, 760, 900, 901, 1150px (`grep -n '@media'`). Several of these are near-duplicates that likely should be one shared breakpoint: 900 vs. 901px (`app.css:267`/`630`/`2152` vs. `2509`/`2572`/`2715` — min-width/max-width pairs that appear to be the same conceptual boundary expressed twice, once as `max-width:900px` and once as `min-width:901px`, which is correct as an exact-complement pair but only if every "desktop" rule in the file consistently uses 901 and every "mobile" rule consistently uses 900 — worth a single audit pass to confirm no rule accidentally uses 899/900/901 inconsistently), and 600 vs. 640 vs. 760 (`app.css:305`, `663`, `2694`/`3294`) read as ad hoc per-feature choices rather than a shared breakpoint token. There is no `--breakpoint-*` custom property or shared Sass-like constant; each `@media` rule hardcodes its own number. web.dev-adjacent responsive guidance recommends breakpoints driven by where a specific layout actually breaks, tested deliberately — which several of these likely were — but even content-driven breakpoints should converge onto a small shared set once established, so the next page built doesn't invent an eighth number.

</td></tr>
</table>

**Remediation.** Raise `button` and `button.icon-btn`'s computed height/width to at least 44px (e.g., `min-height: 2.75rem` on the base button rule, `width/height: 2.75rem` on `.icon-btn`) — this is a two-rule change with sitewide reach. Apply `.table-stack` (already built, already proven on admin.html) to the eleven other `<table>`-based templates listed above; this is mechanical (add the class, add `data-label` attributes per the pattern `.table-stack` already documents) and should be the first item in the implementation pass. Consolidate the seven breakpoints toward a documented set (e.g., 600 / 900 / 1150) and note in a CSS comment which conceptual tier each one represents.

---

### 3. Navigation & information architecture

**Principle.** Jakob's Law holds that users spend most of their time on *other* products, so they bring those products' mental models with them — a product that breaks from its own established navigation convention forces users to re-learn it (lawsofux.com, Jakob's Law). Inside one product, NN/g's consistency heuristic applies this internally: the same action (e.g., "go back to the top-level nav") should look and behave the same everywhere in the app (NN/g, "[Consistency and Standards](https://www.nngroup.com/articles/consistency-and-standards/)").

**What's actually there.** This is one of the stronger parts of the codebase. `icons.html` defines a genuinely well-thought-out nav architecture with real documentation: `desktop-sidebar` (persistent sidebar ≥901px) and `global-tabbar` + `site-nav-fullpage` (bottom tab bar + "More" sheet <900px) are explicitly two renderings of *one* navigation model, not two different information architectures — the code comments at `icons.html:36–44` and `icons.html:168–177` explicitly reason through why Admin is gated differently in each, why Profile appears as a tab in one and a sheet row in the other, and reference a specific bug report ("reported back as still unusable on mobile") that drove a fix. That's a maturity signal most incrementally-built apps don't have.

**Findings:**

<table>
<tr><td><span class="sev sev-med">MEDIUM</span></td><td>

Despite the shared `desktop-sidebar`/`global-tabbar` primitives, several feature areas layer their *own* secondary tab strip on top with independent markup and styling conventions: `.catalog-view-tab` (`app.css:3667`), `.profile-tab-btn` (template-level, with inline Go-template conditionals for active state rather than the `data-path` + `active` class pattern `desktop-sidebar-script`/`global-tabbar-script` already use), band/community/admin sub-tabs each apparently hand-rolled per page. None of these reuse the active-state JS pattern (`data-path` matching against `location.pathname`) that the two *global* nav components share — each secondary tab strip appears to compute or render its active state through its own one-off path, multiplying the number of "how does this app mark the current section" conventions a reader has to learn from two (reasonable: top-level desktop vs. mobile) to five-plus.

</td></tr>
<tr><td><span class="sev sev-low">LOW</span></td><td>

The `home-link` partial's own doc comment (`icons.html:21–26`) notes it was created specifically because "nearly every page template duplicated [the home link] with drift (plain '&larr; Songbooks', '&larr; Books', a styled 'back-link' reading 'Home' — three spellings of the same link)." This is good retroactive cleanup, but it's evidence the same drift pattern is likely still live in other shared micro-components that haven't had their own consolidation pass yet (e.g., the "back to band" / "back to community" breadcrumbs, which the comment explicitly says were *not* touched: "pages whose header instead backs up to a parent resource ... keep their own back-link").

</td></tr>
</table>

**Remediation.** Extract one shared "section tabs" partial + the existing `data-path`/active-class JS pattern, and migrate `.catalog-view-tab`, `.profile-tab-btn`, and the band/admin sub-tab strips onto it. Do a second micro-pass (one grep, similar to the `home-link` consolidation) specifically for "back to parent resource" breadcrumb drift across band/community/project headers.

---

### 4. Forms & interaction feedback

**Principle.** NN/g's error-prevention heuristic calls for a confirmation step before any action with serious, hard-to-reverse consequences, and separately warns that error/confirmation messaging needs consistent tone and timing to stay legible and non-hostile ("[Hostile Error Messages](https://www.nngroup.com/articles/hostile-error-messages/)" — error prevention is heuristic #5: "eliminate error-prone conditions, or check for them and present users with a confirmation option before they commit to the action"). Consistency of *how* that confirmation is presented matters as much as that it exists.

**Findings:**

<table>
<tr><td><span class="sev sev-med">MEDIUM</span></td><td>

Every destructive-action confirmation in the app uses the native `confirm()` dialog — 11 call sites across `admin.html`, `admin_person.html`, `band_members.html`, `profile.html`, `jam_roster.html`, `school_admin.html`, `variant_history.html`, `audio-player.js`, `song.js`, and `project.js`. The *consistency* of always using `confirm()` is actually good (not a mixed pattern) — but the native dialog can't carry the app's own visual language (no token colors, no custom copy styling, inconsistent rendering per browser/OS), and at least one message currently ships a developer-facing caveat straight to the end user: `admin.html:66` — `confirm('Remove {{.Username}}? (not implemented yet)')`. A user who clicks "OK" on that dialog is told the feature doesn't work *inside the confirmation itself*, which reads as an interaction bug, not a copy nit — the resulting button should probably be removed or disabled instead of wired to a no-op with a native alert admitting as much.

</td></tr>
<tr><td><span class="sev sev-low">LOW</span></td><td>

No shared loading/spinner/skeleton CSS exists anywhere in the stylesheet (`grep -n 'spinner|loading|@keyframes'` returns only one unrelated keyframe, a chart-cursor blink). For an app with real async operations (YouTube audio download, PDF export, passkey registration), the absence of *any* shared in-progress indicator means each feature either has no loading feedback or implements its own ad hoc text-swap, which is itself a consistency gap worth flagging even though no single instance was severe enough to inspect individually in this pass.

</td></tr>
<tr><td><span class="sev sev-low">LOW</span></td><td>

Empty states are inconsistent in both markup and tone: `.hint` text-only ("No active jam sessions yet.", `jams_index.html:39`), a `<td colspan>` hint row (`notifications.html:39`), and a dedicated `.attention-empty` component with a call-to-action link (`band.html:179,212`, "No songs yet — add one from the repertoire page →"). The last pattern is the best one — it tells the user what to do next, not just that nothing is there — but it's used in only one template family.

</td></tr>
</table>

**Remediation.** Fix the `(not implemented yet)` confirm copy immediately — either remove the button or disable it with a tooltip; that's a one-line, zero-risk fix independent of any broader redesign. Standardize on `.attention-empty`'s "tell them what to do next" pattern for every empty state in the app. Add one shared `.spinner`/`.btn-loading` component for async actions.

---

### 5. Accessibility

**Principle.** WCAG 2.2 SC 1.4.3 Contrast (Minimum, Level AA) requires 4.5:1 for normal text and 3:1 for large text (≥18pt, or ≥14pt bold) against its background (W3C, "[Contrast (Minimum)](https://www.w3.org/WAI/WCAG20/Understanding/contrast-minimum.html)," carried unchanged into 2.2). SC 2.4.7 Focus Visible requires any keyboard-operable UI to have a visible indicator when it has focus. These overlap directly with this app's real usage pattern — musicians glancing at a phone in variable, often low, stage lighting need *more* contrast margin than a desk-bound reader, not less.

**Findings — computed directly from the `:root` token values (WCAG relative-luminance formula):**

<table>
<tr><td><span class="sev sev-high">HIGH</span></td><td>

`--warn` (`#b8762a`) text on `--warn-bg` (`#f5e3c4`), light theme — contrast ratio **2.94:1**. Fails AA even at the large-text threshold (3:1), and badly fails normal-text AA (4.5:1). This pair is used for warning badges/status text (`app.css` warn token group, `:root:44-83`) — exactly the color a low-vision or glare-affected user needs to read reliably.

</td></tr>
<tr><td><span class="sev sev-high">HIGH</span></td><td>

`--ok` (`#8a7a2e`) text on `--ok-bg` (`#f2ebc6`), light theme — **3.56:1**. Fails normal-text AA (4.5:1); passes only the large-text threshold. Used for success/confirmation status (e.g., the wizard's "done" indicator path, `app.css:3778` `.wizard-dot.done { background: var(--ok); }`, and likely other "approved"/"accepted" badges across band/community admin flows).

</td></tr>
<tr><td><span class="sev sev-med">MEDIUM</span></td><td>

`--fg-faint` (`#948a79`) on `--bg` (`#f6f1e7`), light theme — **3.02:1**. This token is explicitly the "faintest" tier of body text (used for secondary metadata, timestamps, hints per its naming), and it fails normal-text AA; it just barely clears large-text AA. Dark-mode's equivalent pair (`#9c8f78` on `#1d1712`) fares better at 5.59:1 — the light theme is the one that needs adjustment.

</td></tr>
<tr><td><span class="sev sev-med">MEDIUM</span></td><td>

`app.css:338–339` and `app.css:800` both set `outline: none` on a focusable element (`#band-name-display`, `#bpm-input`) and replace it only with a background-color change, not a visible outline/ring. A background tint alone is a weaker focus signal than an outline (fails the spirit of WCAG 2.4.7/2.4.11, and definitely fails the AAA non-text-contrast guidance for focus indicators) — a sighted keyboard user tabbing through the editor or metronome page may not reliably notice which field currently has focus, especially against `--bg-sunken`, whose contrast against `--bg` is deliberately subtle.

</td></tr>
<tr><td><span class="sev sev-low">LOW</span></td><td>

Only 4 explicit `:focus-visible` rules exist in the entire stylesheet (`app.css:168, 338, 2875, 3171`), each scoped to one specific component rather than a general `:focus-visible` ring applied once to all interactive elements. Everywhere else, focus styling falls back to each browser's own default outline, which is functionally fine but inconsistent in appearance across the ~25 button classes cataloged in Finding 1 — some will show a crisp browser-default ring, others (anything with `border-radius: 999px`, e.g. `.icon-btn`) will show a visually clipped or odd-looking default ring rather than one designed for that shape.

</td></tr>
<tr><td><span class="sev sev-low">LOW</span></td><td>

Icon-only controls generally do carry `aria-hidden="true"` on the inner `<svg>` correctly (confirmed in `icons.html`'s icon definitions) and several nav landmarks have `aria-label` (`desktop-sidebar aria-label="main navigation"`, `global-tabbar aria-label="main navigation"` — note: both landmarks share the identical label text, which is harmless but means a screen-reader user navigating by landmark can't distinguish them by name alone if both ever render in the same DOM at different breakpoints simultaneously, which they do — both are always in the DOM, just CSS-hidden). Icon-only *buttons* (e.g. `button.icon-btn` instances without visible text) were not confirmed in this pass to consistently carry an accessible name (`aria-label` or `title`) — worth a dedicated sweep in the implementation phase since this pass sampled rather than read every icon-button call site.

</td></tr>
</table>

**Remediation.** Darken `--warn` and `--ok` (and verify `--danger`, which passes today at 5.46:1 but should be rechecked after any palette-wide adjustment) until both clear 4.5:1 against their `-bg` pairs in light mode — this likely only needs each hue's lightness lowered a few percent, not a hue change, so the palette's character is preserved. Replace the two `outline: none` resets with a real `outline`/`box-shadow` focus ring. Add one general `:focus-visible` rule (outline + offset, using the existing dashed-outline pattern at `app.css:2992` as the reference implementation, which is already well-built) so every interactive element gets a consistent, visible ring without per-component opt-in. Audit all `icon-btn` call sites for an accessible name.

---

## Steps to reach compliance

Ordered for mechanical execution by whoever runs the follow-up implementation pass. Each phase is scoped to not block on the others.

**Phase 0 — Zero-risk one-liners (do first, ship same day)**
1. Fix `admin.html:66`'s `confirm('Remove {{.Username}}? (not implemented yet)')` — disable/remove the button instead of shipping a no-op.
2. Replace the three stray hardcoded hex colors (`app.css:978`, `3770`, and the `#fff` at `168`/`479`) with their token equivalents (`var(--danger)`, `var(--bg-raised)`, `var(--accent-contrast)`).
3. Fix `app.css:3807–3808`'s `--line`/`--card`/`--ink`/`--text-muted` references to point at real tokens (`--border`/`--bg-raised`/`--fg`/`--fg-muted`) — currently silently broken in dark mode.

**Phase 1 — Design tokens and spacing scale**
4. Darken `--warn` and `--ok` (light theme) until text-on-bg clears 4.5:1; re-verify `--danger` and all dark-mode pairs after.
5. Define a documented spacing scale (e.g., 4/8/12/16/24/32/48px as `--space-*` tokens) and migrate the clearest inline-`style` offenders (`admin.html:139`, `admin_person.html:49,68`, `play.html:593,599,600,603`) onto it via small modifier classes (`.card--narrow`, etc.).
6. Consolidate the 25+ one-off button base classes onto a documented `.btn` family with `.btn-sm`/`.btn-icon`/modifier conventions; keep `primary`/`danger`/`ghost-btn` as-is since those are already working correctly.

**Phase 2 — Responsive/touch-target sweep**
7. Raise base `button` and `button.icon-btn` sizing to ≥44px (two rules, sitewide reach) — `app.css:151-153` and `169`.
8. Apply the existing `.table-stack` pattern + `data-label` attributes to the eleven templates identified as still using bare `<table>`: `school_admin.html` (highest priority — same shape as the already-fixed `admin.html`), `band_members.html`, `band_proposal.html`, `band_proposals.html`, `jam_songs.html`, `jams_index.html`, `jam_add_songs.html`, `notifications.html`, `variant_history.html`, `schools.html`, `school.html`.
9. Consolidate the seven ad hoc `@media` breakpoints (480/560/600/640/760/900/901/1150) toward a documented 3-tier set; add a comment at each surviving breakpoint naming which tier it is.

**Phase 3 — Navigation consistency**
10. Extract a shared "section tabs" partial + reuse the `data-path`/active-class JS pattern from `desktop-sidebar-script`/`global-tabbar-script` for `.catalog-view-tab`, `.profile-tab-btn`, and band/admin sub-tab strips.
11. Repeat the `home-link`-style consolidation grep for "back to parent resource" breadcrumbs across band/community/project headers.

**Phase 4 — Accessibility hardening**
12. Replace the two `outline: none` resets (`app.css:338-339`, `800`) with a visible focus ring.
13. Add one general `:focus-visible` rule for all interactive elements, using `app.css:2992`'s dashed-outline pattern as the template.
14. Sweep every `icon-btn` call site for an `aria-label`/`title`; add where missing.
15. Give `desktop-sidebar` and `global-tabbar` distinct `aria-label`s (they're both always in the DOM simultaneously, just CSS-hidden by breakpoint).

**Phase 5 — Feedback-state polish (lower priority, do last)**
16. Build one shared `.spinner`/button-loading-state component and wire it into the async actions that currently have none (YouTube audio download, PDF export, passkey registration).
17. Standardize every empty state on the `.attention-empty` "tell them what to do next" pattern currently used only in `band.html`.

---

## Remediation log (2026-11-05, same-day follow-up pass)

All 5 phases of the punch list above were implemented in six commits (one for the audit itself, one per phase) on `main`, each gated on `go build ./... && go test ./...` passing and a visual check in a local preview (desktop + mobile emulation) before moving to the next phase. What follows is what actually landed, and — since a mechanical punch list still meets real tradeoffs once you're in the code — what was deliberately *not* done and why, so the gap doesn't read as an oversight later.

**Done as specified:** Phase 0 (all 3 items); Phase 1 items 4 (contrast), 5 (spacing scale + the listed inline-style offenders); Phase 2 (all 3 items — `.table-stack` rolled out to 10 of the 11 named templates, plus `band_proposal.html` which had the same bare-table shape on closer inspection); Phase 4 (all 4 items, including a sweep that turned up no actual icon-btn accessible-name gaps).

**Done, adapted from the letter of the item:**
- Phase 2 item 8: `jam_songs.html`'s per-song table was *not* given `.table-stack` — its `<thead>` holds the song's own title (`colspan="3"`), and `.table-stack` hides `<thead>` below 600px, which would delete the song title on mobile instead of fixing anything. Applying the class verbatim would have made this template *worse*, not better, so it was left as-is pending a markup restructure (move the title out of `<thead>`) that's a real change, not a mechanical one.
- Phase 2 item 9: breakpoints were **documented**, not **merged**. Direct read of all 16 `@media` rules confirmed the 900/901 and 760/761 pairs are already used as exact, consistent complements (no accidental 899/900/901 drift) — so the audit's own hedge ("worth a single audit pass to confirm") came back clean. The 8 raw pixel values were grouped into 3 named tiers plus one standalone widescreen case in a new `:root` comment, but not collapsed onto fewer literal numbers: several are tuned to their own component's real content-driven break point (most say so in their own existing comment), and merging them without re-testing every affected layout would trade a documented, intentional number for an untested coincidence.
- Phase 5 item 17: 5 of the ~15 "No X yet" empty states found were converted to `.attention-empty`; the rest were left alone because they either have no real next step to point at (a past-events list, a public read-only setlist view) or already sit directly beneath their own visible action button, where swapping the CSS class would be cosmetic only, not a real "tell them what to do next" improvement the way the Phase 5 principle calls for.

**Deliberately deferred, not done:**
- Phase 1 item 6 (consolidate 25+ button base classes onto a documented `.btn` family). Flagged LOW severity in the audit, and several of those classes (`vote-btn`, `accept-btn`, `jam-pick-btn`, ...) are live JS event-handler selectors — renaming them is a real refactor with functional-regression risk across every page with a bound click handler, for a naming-only win since the `primary`/`danger`/`ghost-btn` modifiers they already compose with render correctly today.
- Phase 3 item 10 (extract one shared "section tabs" partial reusing the sidebar's `data-path`/active-class JS for `.profile-tab-btn` and the admin/band sub-tab strips). On inspection these are two different interaction models, not one component used twice: the sidebar/tabbar pattern matches `location.pathname` to highlight a link to a *different page*; `.profile-tab-btn` and `#admin-tabs` instead toggle which panel is visible *within the same page*. A true shared partial means generalizing both into one abstraction — a structural refactor with its own regression surface, not an "add a class" change.
- Phase 3 item 11 (full breadcrumb-drift grep) was done as a grep and came back almost entirely clean — one real drift fixed (`metronome.html`'s `&larr;` text arrow, now the shared `icon-back` SVG like every other back-link). Two public, no-sidebar jam pages and two in-page JS view-toggle buttons (not navigation links at all) were left alone as a different context, not the same drift pattern.
- Phase 5 item 16's PDF-export leg: PDF export is a plain `<a download>` link, not a fetch-driven async action — there's no in-flight JS state to attach a spinner to without first adding fetch-based download machinery that didn't exist before, which is a bigger change than "wire in the existing component." The spinner component itself was still built and wired into the other two named actions (YouTube download, passkey registration).

**One opportunistic fix beyond the literal list:** while in `school_admin.html`/`schools.html`/`school.html`/`band_proposal.html` for the `.table-stack` work, three more `style="max-width:…px"` cards of the same drift pattern Phase 1 item 5 was already fixing were caught and moved onto the `.card--narrow`/`.card--md` modifier classes rather than left for a future pass.
