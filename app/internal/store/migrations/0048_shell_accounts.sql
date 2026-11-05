-- Personal invites replace the old shared "group invite" roster link
-- (pick your name from a list) with a pre-created "shell" person: the
-- inviter sets the person's memberships (band_members/school_members
-- rows, however many bands/communities) up front, against a real
-- person_id, before any invite link even exists — so the roster is
-- accurate immediately ("Roland, guitar/bass/singer, core" is a real
-- row from the moment he's added, not a pending slot) and one invite
-- link can cover several memberships at once, since they're already
-- just ordinary band_members/school_members rows for that person_id.
--
-- is_shell marks a person row created this way but not yet claimed —
-- password_hash being NULL isn't a reliable signal on its own (a real
-- account that only ever uses magic-link login also has no password).
ALTER TABLE people ADD COLUMN is_shell boolean NOT NULL DEFAULT false;

-- invites.person_id, when set, makes this a personal invite: claiming
-- it completes THIS existing (shell) person's account (sets username/
-- password/email, clears is_shell) rather than creating a brand-new
-- person row. NULL keeps the original "plain signup invite" behavior
-- (admin-panel, no pre-attached memberships) working unchanged.
ALTER TABLE invites ADD COLUMN person_id bigint REFERENCES people(id) ON DELETE CASCADE;
