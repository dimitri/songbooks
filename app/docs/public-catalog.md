# Public, logged-out catalog browsing — proposal

Status: **proposed, not built**. Everything in this app today sits behind
`requireAuth` (see `internal/httpapi/auth.go`) — there is no "logged out"
rendering path anywhere, and no "public" concept in the data model at all
(`variant_visibility` is an *ownership* scope — global/user/band/jam/school
— not a public/private flag; see migrations 0008/0011/0014/0024). This
is a proposal for a new, narrow, read-only surface: a visitor with no
account can browse a catalog of songs/projects/bands that their owners
have explicitly opted in to showing. Nothing here is built yet — this is
the design to react to before any of it is implemented, same spirit as
`mp3-reference-audio.md`.

## 1. What "public" actually means here — explicit opt-in, per project/band

**Decision: public is off by default, everywhere, and turned on one
project or band at a time by whoever owns it.** Nothing becomes
reachable by a logged-out visitor just because this feature ships. A new
boolean column:

```sql
ALTER TABLE projects ADD COLUMN is_public boolean NOT NULL DEFAULT false;
ALTER TABLE bands    ADD COLUMN is_public boolean NOT NULL DEFAULT false;
```

Toggled from the project/band's own settings (projects: owner role only,
matching every other owner-gated project action; bands: any current
member, matching this codebase's existing "no owner/admin tier for
bands" design — see `requireBandMember`'s doc comment in `bands.go`).
No instance-wide "make everything public" switch, and no admin override
that flips it on without the owner's action — this has to be a decision
the person who put the material there actually makes.

## 2. What a logged-out visitor actually SEES — metadata only, not charts

This is the real decision in this proposal, and the one with actual legal
weight, so it gets stated as plainly as `mp3-reference-audio.md` states
the YouTube-download one:

**A public project/band page shows its song LIST — title, artist, key,
tempo, time signature, and (if set) a cover-art/reference link — never
the chord chart or lyrics themselves, regardless of the owner's `is_public`
toggle.** The chart/lyrics view (`/b/{book}/{stem}`, the PDF export, the
reference-audio player) stays behind `requireAuth` unconditionally. Two
separate reasons, stated separately because they're different kinds of
risk:

- **Copyright.** A chord chart is a derivative work of someone else's
  song; lyrics are the song's own copyrighted text verbatim. An
  invite-only, friends-only deployment showing these to people the
  operator personally knows is the informal risk posture
  `mp3-reference-audio.md` already names and accepts for *that* feature.
  Showing the same content to anyone on the open internet, with no
  account and no relationship to the operator, is a fundamentally
  different exposure — the kind a rights holder's takedown request
  actually targets, not a gray area this doc can wave away. Metadata
  (a song exists, by this artist, in this key) is not the protected
  expression; the chart/lyrics content is.
- **This band's/project's own privacy.** A band's working repertoire,
  arrangement notes, and gig setlists are often not things the band wants
  a stranger reading even if copyright weren't a factor (an unreleased
  arrangement, a setlist for an upcoming private event). The "list only"
  boundary protects that too, independent of the copyright question.

If a project/band ever wants to publish actual chart content publicly
(e.g. a band's own **original** songs, where they hold the copyright and
have a real reason to want it public), that is a **separate, explicit,
per-song decision** — not a side effect of the `is_public` project/band
toggle. Not scoped further here; flagged as a real future ask worth its
own proposal if the user wants it, rather than smuggled into this one as
an assumption.

## 3. Routes and rendering — logged-out templates, not a flag on the existing ones

**Decision: new routes, new (small) templates — not an `if .Me` branch
threaded through the existing project/band pages.** The existing pages
(`project.html`, `band.html`, `band_catalog.html`, ...) assume a
logged-in `.Me` throughout (the sidebar, the tab bar, role-gated action
buttons) — retrofitting every one of them to also render sanely with no
session is a correctness hazard (one missed `{{if .Me}}` leaks a
null-pointer-shaped bug, not a graceful degrade) for a page that, by
definition, is reachable by anyone, including automated scanners. A
small number of dedicated public views is safer and clearer to review:

```
GET /explore                  — the public catalog landing page: public
                                 projects and public bands, name + a short
                                 stat ("14 songs") each, nothing else
GET /explore/p/{slug}          — one public project's song list (metadata
                                 only, per §2) + a "want the chart? log in
                                 or ask to join" message, no chord/lyric
                                 content, no edit affordance anywhere
GET /explore/b/{id}            — the band equivalent
```

Kept under `/explore/...` rather than reusing `/b/{book}` or
`/bands/{id}` so there is never a question of "which version of this URL
is this" — a public URL is visually and structurally a different page,
not the member page with pieces hidden.

**Auth middleware: a new `optionalAuth`, not a change to `requireAuth`.**
`requireAuth`'s existing contract (redirect/401 if no session) stays
exactly as it is for every page that has one today — this is additive,
not a loosening of anything. `optionalAuth` loads a session if present
(so a logged-in visitor on `/explore` still sees their own header/nav
chrome) but never requires one:

```go
// optionalAuth loads the session into context if one exists, like
// requireAuth, but never redirects/401s when there isn't one — only for
// the small set of /explore routes a logged-out visitor can reach at
// all. Every other route keeps using requireAuth/requireRole unchanged.
func (s *Server) optionalAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
				ctx := context.WithValue(r.Context(), personCtxKey, p)
				next(w, r.WithContext(ctx))
				return
			}
		}
		next(w, r)
	}
}
```

`personFromContext(r)` already returns `nil` on a missing/invalid
session (see its current implementation) — every handler built on this
already has to handle that case, so no new nil-safety convention is
needed, just routes that are allowed to hit it with no cookie at all.

## 4. Query shape — explicit allow-list, not "everything minus private"

`ListProjectsForPerson`/`ListBooks` and the band equivalents are
person-scoped or admin-scoped today; the public listing needs its own
narrow query rather than reusing and then filtering one of those:

```sql
SELECT slug, name FROM projects WHERE is_public ORDER BY name;
SELECT id, name  FROM bands    WHERE is_public ORDER BY name;
```

and the song list for one public project/band reuses the existing
`ListSongsOrdered`/repertoire query verbatim — it's the same "what songs,
in what order" data the member page already shows, just rendered by a
template that stops at title/artist/key/tempo and never reaches the song
body at all (the handler for `/explore/p/{slug}` never calls
`LoadSong`'s body-returning path — the safest way to guarantee chart
text never reaches this response is to never fetch it in the first
place, not to fetch it and trust a template to not print it).

## 5. Abuse/operational surface — new, because this is genuinely new

Every other page in this app sits behind a login, so today there is no
unauthenticated traffic to think about at all. `/explore/...` changes
that in a small but real way:

- **Rate limiting.** Even metadata-only, an open `/explore/p/{slug}`
  endpoint is scrapeable at volume. A simple per-IP rate limit (e.g. a
  small in-memory token bucket keyed on `r.RemoteAddr`, since this is a
  single-process deployment with no load balancer to complicate that) on
  the `/explore/...` route group specifically — not needed anywhere else,
  since every other route already requires a session.
- **`robots.txt`.** Decide up front whether `/explore/...` should be
  indexable. Default recommendation: allow it — the whole point of a
  public catalog is to be findable — but this is the operator's call,
  not an assumption to bake in silently.
- **No account enumeration.** `/explore/p/{slug}` for a slug that exists
  but has `is_public = false` must 404 exactly like a slug that doesn't
  exist at all (same "don't leak existence" posture `requireRole`'s own
  doc comment already states for private-project 403s) — never a
  different error for "exists but private" vs "doesn't exist."

## 6. Build order

1. Migration: `is_public` on `projects` and `bands`.
2. Owner-facing toggle: one checkbox on the project settings area /
   band members page ("Visible in the public catalog"), POST handler,
   no other UI change.
3. `optionalAuth` middleware + the three `/explore/...` routes and their
   dedicated templates (metadata-only, per §2/§4) — no chart/lyric
   content ever fetched in these handlers.
4. Per-IP rate limiting on the `/explore/...` route group.
5. `robots.txt` decision + `<meta name="robots">` on the pages themselves
   if the answer is "not indexable."

Steps 1–3 alone deliver the actual feature; 4–5 are the "now that this is
reachable by anyone" hardening pass, worth doing before announcing the
feature publicly but not blocking a first internal trial of it.

## Revisit at scale

- **If any project/band ever wants real chart content public** (most
  plausibly: a band's own original material), that's a separate,
  explicit, per-song opt-in — never implied by the project/band-level
  `is_public` flag this proposal scopes. Needs its own copyright-aware
  design pass (at minimum: the song's `music_type` already distinguishes
  originals from covers — see `handleSetBandMusicType` — which is the
  natural gate to require "originals only" on, not an assumption to make
  here).
- **If `/explore` traffic becomes real traffic** (not just the operator
  checking their own public page), the per-IP rate limit in §5 stops
  being "good hygiene" and becomes load-bearing — revisit it as a real
  capacity question, possibly moving to a reverse-proxy-level limiter
  instead of in-process.
- **If this instance ever hosts material the operator doesn't personally
  vouch for** (a multi-tenant SaaS posture rather than "my own bands and
  friends"), the honor-system "owner ticks a box" opt-in in §1 needs a
  real moderation/reporting path behind it — a public catalog with no
  way for a third party to flag something is a liability a single-
  operator personal tool doesn't have today but a hosted product would.
