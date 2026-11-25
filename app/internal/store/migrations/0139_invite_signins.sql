-- A personal link keeps opening for its person until it expires (decided
-- 2026-11-25: people use several devices, and open the same link again
-- on each). Its first use makes the account (used_at, migration 0138);
-- every later use is a sign-in, with that account's password or passkey,
-- never by the link alone. These count them, for the admin and the
-- rosters to show ("signed in with it 3 times, last 25 November"):
--
--   * signins: how many times someone signed in through the link;
--   * last_signin_at: the last time; NULL until the first.

ALTER TABLE invites
  ADD COLUMN signins integer NOT NULL DEFAULT 0,
  ADD COLUMN last_signin_at timestamptz;
