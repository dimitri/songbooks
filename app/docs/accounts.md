# Accounts: the email is the identity

An account is its email. There are no usernames (decided 2026-11-24,
migration 0135): an email is all an account needs, and people are shown
everywhere by their display name.

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

## Making an account

Every way in asks for a name ("Your name", the display name) and an email:
an invite link (`/invite/…`), a band or community link (`/join/…`),
registering for an event. A placeholder added by name to a roster (a
*shell*, migration 0048) is claimed through their personal link with an
email and a password; their name is the one they were added with (they
change it on their profile).

## Adding someone who has an account

A band's Members page and a community's Admin page add an account by
**email** (any account; an email nobody uses is refused — leave it blank
for a link to send them, which is how a newcomer makes an account), or by
**picking a name** among the people the adder already plays with: the
current members of their current bands and communities, not The Big
Songbook (`store.ListPeopleKnownTo`; the API takes `email` or `personId`).
Each name in that list carries where the two share a group ("Marc · Nord
Express"), which also tells two people of the same name apart.

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

Migration 0135 made it nullable; nothing reads it any more, and nothing
writes it (new accounts and claimed placeholders leave it NULL; `sbdemo
anonymize` empties it). It keeps the old names for now. **Dropping it is a
later, separate step** (a migration `ALTER TABLE people DROP COLUMN
username`), once nothing outside the app depends on it: old database dumps
and songexport backups (`cmd/songimport` still reads an older export's
`username` to find its person directories, which are now `person-<id>`).
