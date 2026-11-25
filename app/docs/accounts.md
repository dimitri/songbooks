# Accounts: the email is the identity

An account is its email. There are no usernames (decided 2026-11-24,
migrations 0135 and 0136): an email is all an account needs, and people
are shown everywhere by their display name. Inviting someone takes only
their name; joining takes their email.

## Logging in

- **Password**: the login form asks for the email and the password
  (`handlePasswordLogin`). The email is matched without regard to case.
  Every failure (an unknown email, something that isn't an email, an
  account without a password, a wrong password) gets the same answer, one
  bcrypt comparison either way: the form doesn't tell whether an email
  has an account.
- **Login link**: the email gets a one-time link; the page says the same
  thing whether or not the email has an account.
- **Passkey**: no identifier at all. A passkey manager lists the passkey
  under the account's email (`WebAuthnName`).

An account with a password but no email can't log in any more. The admin
page lists them at the top of *People*, and the server says so in its log
at every start, until each gets one: `songbooks --set-email <id> <email>`.
Staging had one on 2026-11-24 (id 3, "Band Mate").

## Adding someone: a name

A band's Members page and a community's Admin page add someone by
**name** (and, for a band, the instrument and the role; for a community,
the role): they're on the roster straight away, as a placeholder (a
*shell*, migration 0048), with a **personal link** to send them
(`/invite/…`, 30 days, "New link" makes another; once they've joined
with it, it still opens for them until then, on their other devices:
*Coming back with a personal link*, below). Nobody types an email
for someone else: there's no adding by email, and nothing on these pages
tells whether an email has an account (decided 2026-11-24; the add routes
take a `personId`, never an email).

Someone who already has an account can also be **picked by name** among
the people the adder already plays with: the current members of their
current bands and communities, not The Big Songbook
(`store.ListPeopleKnownTo`, `POST …/members {personId}`). Each name in
that list carries where the two share a group ("Marc · Nord Express"),
which also tells two people of the same name apart. They're added
straight away.

The other ways in stay: a band or community open link or QR code, a
community's group invite with names to pick (`/join/…`), registering
for an event, a site admin's plain invite (`/invite/…`, from the admin
page: an account with no membership).

A band's **nameless one-off link** is gone (decided 2026-11-24): the
Members page made one when nobody was picked ("Create invite link"), an
`/invite/…` with no name, single use, 7 days. Invites are made by name
(a placeholder and its personal link) or with the band's open link or
QR code, per role. Its route (`POST /api/bands/{id}/invites`) and
`songbooks --create-band-invite` went with it, and one made before
isn't taken any more (`GetInvite` and accepting skip an `invites` row
with a band and no person; staging had none unused that day). A
community never had one.

## Joining: an email

Every link asks the person to **sign up or sign in with their email**:

- **Signed in already**: they just join, as themselves ("Join as …?",
  with a way to log out on a browser that isn't theirs).
- **A new account**: their email and a password on the link's page (and
  their name, except on a personal link, which has it). On a personal
  link the account *is* the placeholder: claiming it sets its email
  (`ClaimPersonalInvite`).
- **An account they have**: "Log in to join" (on an event, "Log in to
  register") remembers the link for 15 minutes (`pendingJoinCookie`) and
  goes to the login page, by password or by an emailed link; back on
  `/home`, the join is finished (`resumePendingJoin`). A personal link's
  placeholder then **merges into their account** (`ClaimPersonalInviteAs`:
  everything that named the placeholder names the account; where the
  account has the same already, a membership in that band, an
  instrument, its own stays) and the placeholder goes. An email that has
  an account is never taken for a new one: the form says to log in to
  join instead.

## Coming back with a personal link

A personal link isn't used up by joining (decided 2026-11-25: a Mercy
member signed up through his link, opened it on another device, and it
was dead). It keeps opening **for that one person until it expires**
(30 days from when it was made; the pages show the date). Its first use
makes the account (above: `used_at`, migration 0138, is when); every
later use is that person coming back (`returning_link.go`,
`store.GetReturningLink`: the link's person has an account, the link
hasn't expired):

| Who opens it | What they get |
|---|---|
| signed out | "Welcome back, Guillaume": **Continue as Guillaume** with the account's **password**, or **their passkey**; the account's email shown masked (`g•••@gmail.com`; a domain that isn't a common provider's is masked too, `g•••@d•••.fr`), never in full. Then the band or community the link took them to (`joined_band_id` / `joined_community_id`, else their first band, else first community; home when they've left it). "Not you?" goes to the login page. |
| signed in as that person | straight to that band or community |
| signed in as someone else | "This link is Guillaume's. You're signed in as Marc." Log out (back to the link) or Go home. No merge of an account into another. |
| expired or revoked | the dead link's page (410): "It has expired. You joined Mercy with it." and Log in |

- **The link never signs anyone in by itself**: it is shareable (a
  WhatsApp message, a QR code). Opening it sets no session; only the
  account's password or passkey does.
- A wrong password gets the login page's one answer ("That email and
  password don't match"), one bcrypt comparison whatever the account.
  **Wrong passwords through personal links are limited per account**: 10
  in 15 minutes, then "Too many tries" (429) until the window ends, the
  right password included (in memory; a restart forgets). The login page
  itself has no such limit yet.
- **It can't be claimed twice**: the claims read `used_at IS NULL` and
  `is_shell` (row-locked), so a later use can't make another account,
  nor change the email or the password; whatever else its form carries
  is ignored.
- The passkey is the login page's own ceremony (any passkey of the
  site's); back on the link, its person goes on to the band, anyone else
  gets "This link is …'s". `?link=` on the finish counts the sign-in on
  the link when the passkey is its person's.
- Each sign-in through the link is counted (`signins`, `last_signin_at`,
  migration 0139).
- The first use's page says to keep the link ("until 25 December it
  brings you back in on your other devices too").

**Seen by those who invite**: the admin's person page and the rosters
(a band's Members page for its core members, a community's Admin page,
"Joined, link still open") show it as "used to join on 25 November ·
still opens for Guillaume until 25 December · signed in with it 3 times,
last 2 December", with Copy link, QR code, **New link** (a new one in its
place, the old one stops: a leaked link) and **Revoke** (it expires now;
`DELETE /api/people/{id}/personal-invites`, the same people who may make
one). A roster left open since they joined still copies the same link,
and its "Get link" gives it back, not a dead one. Once expired or
revoked it is over: no new one (409), they log in from the login page
(a forgotten password: a site admin's login link).

## A password, while no login email is sent

No login email actually reaches anyone yet (the console mailer: it
logs, and keeps a copy in the outbox; `--smtp-url` isn't built), so an
account without a password would be locked out once its session ends
(decided 2026-11-24: no email capabilities yet). So:

- **Every way to make an account or claim a placeholder asks for a
  password**, typed twice: a personal link, a band or community open
  link, a community's group invite (a name or "someone else"), a site
  admin's plain invite, and **registering for an event** (optional
  until then). The rule (`password_rule.go`, the profile page's too):
  at least 8 characters, at most 72 bytes (bcrypt's limit), the two
  the same. The server checks it, whatever the form did; the pages put
  its answer under the field it's about.
- **The login page leads with the password** (then the passkey, then
  "Email me a login link", which still works as before). The check is
  the mailer's (`mailer.Delivers`: the console one doesn't), so a real
  SMTP mailer, once built, puts the passkey first again and brings back
  "a link we send you" on the join pages, "Forgot your password?" by
  email, and the profile's "you log in with emailed links". Until then,
  a forgotten password is a site admin's login link (a Members page's
  "Login link"), then a new password on the profile page.
- **Accounts with an email but no password nor passkey** (event
  registration made them) can't log back in: the admin page lists them
  at the top of *People*, and the server says so in its log at every
  start, while no login email is sent. A site admin's login link gets
  them in to set a password on their profile, or `songbooks
  --set-password <id>`. Staging had none on 2026-11-24. Their profile
  page says to set one now.

## A site admin's page for one person

`/admin/people/{id}` (`httpapi/admin_person.go`), from the admin's
People list and its invites: the person's state as a chip — **active**,
**hasn't joined yet**, **can't log in: no email**, **can't log in: no
password** (an email but neither a password nor a passkey, while no
login email is sent) —, their way in, every band and community they're
on with that group's own roles (a band's own roles included) and
Remove, an add row, and the site-admin switch (asked before it changes;
never one's own).

- **Hasn't joined yet** is a placeholder (`is_shell`), or an account
  nobody ever got into: no email, no password, no passkey, and no
  session nor used login link, ever (`neverJoinedSQL`: an import's or an
  early roster's name). Its way in is its **personal invite link**: the
  page shows it (made when, by whom, until when; a QR code), Copy link,
  New link (the old one stops working), Revoke. Making the link turns a
  never-joined account into a placeholder, in the same transaction
  (`CreatePersonalInvite`, every caller, `--create-personal-invite`
  too): nothing is taken from anyone, and everything else (claiming,
  rosters, the login link's refusal) goes on reading `is_shell` alone.
  Once the link is claimed the page says **active**, and shows the link
  while it still opens for them (above: *Coming back with a personal
  link*), with New link and Revoke.
- **Someone with an account** gets the admin's **login link** (the
  Members pages' dialog), to set a new password on their profile. No
  page sets someone else's password: `songbooks --set-password <id>`.

## Showing people

The display name, everywhere: rosters, "added by", "decided by",
proposals, stats, managers, the admin pages. No "@name". Two people with
the same name in one list are told apart by what the list already shows
(their instruments on a roster, their role); nothing is added, an email in
particular never (except to site admins, on the admin pages).

## Naming a person on the command line

`songbooks --set-password/--grant-admin/--set-email/--create-personal-invite`,
`SONGBOOKS_DEV_AUTO_LOGIN`, `sbcoach --coach/--student/--person/--actor`,
`sbfiles`/`gpmigrate --actor` and `installscores --as` take an **email or a
numeric person id** (`store.FindPerson`). A name that is neither is
refused: there's no transition period accepting usernames.

## The `people.username` column

Gone: migration 0135 made it nullable and the app stopped reading and
writing it; 0136 dropped it (and its UNIQUE). A database dump from before
restores and migrates as any other (0135 then 0136). `cmd/songimport`
still reads an older export's `username` field, from the JSON only, to
find that export's person directories (now `person-<id>`); nothing writes
it anywhere.
