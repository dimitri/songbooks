-- Links a jam participant's profile to their real account when they're
-- already logged in at join time (see handleJamLandingPage/handleJamJoin) -
-- "another access added to their dashboard" instead of a brand-new
-- disconnected nickname every time the same person joins the same jam.
-- NULL (the only case before this) is the existing fully-anonymous
-- nickname-only participant, untouched.
ALTER TABLE jam_profiles ADD COLUMN person_id bigint REFERENCES people(id) ON DELETE SET NULL;
-- At most one profile per (jam, logged-in person) - re-visiting the same
-- jam link while logged in reuses the existing profile instead of
-- creating a duplicate.
CREATE UNIQUE INDEX jam_profiles_person_unique ON jam_profiles (jam_event_id, person_id) WHERE person_id IS NOT NULL;

-- A second, separate shareable link that grants ORGANIZER status
-- directly to whoever (already has an account and) follows it - kept on
-- its own page/route from the plain participant link
-- (/jam/{code}) specifically so an organizer never shares the wrong one
-- by mistake (the user's own explicit requirement). One token per jam
-- event (not a separate table - jam_organizers already allows several
-- people to hold the role, this is just the invitation mechanism for it).
ALTER TABLE jam_events ADD COLUMN organizer_token text UNIQUE;
