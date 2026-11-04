-- Public-profile fields: a nickname and optional picture to show instead
-- of the account's real display name/username, plus opt-in toggles for
-- whether email/phone are shown on that public profile. picture_url
-- follows the same convention as project_albums.cover_url (migration
-- 0034) — an external URL the person pastes in, not a new upload/asset
-- pipeline — and phone is plain text (no verification), same posture as
-- email before SetEmail existed.
ALTER TABLE people ADD COLUMN nickname text NOT NULL DEFAULT '';
ALTER TABLE people ADD COLUMN picture_url text NOT NULL DEFAULT '';
ALTER TABLE people ADD COLUMN phone text NOT NULL DEFAULT '';
ALTER TABLE people ADD COLUMN share_email boolean NOT NULL DEFAULT false;
ALTER TABLE people ADD COLUMN share_phone boolean NOT NULL DEFAULT false;
