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
(`/invite/…`, 30 days, "New link" makes another). Nobody types an email
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

The other ways in stay: a band's one-off link (`/invite/…`, nameless,
single use), a band or community open link or QR code, a community's
group invite with names to pick (`/join/…`), registering for an event.

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

A new account from a link needs a password: no login email is actually
sent yet (the console mailer, `--smtp-url` isn't built), so an account
without one would be locked out once its session ends. Registering for
an event is the exception (its password is optional, as before).

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
