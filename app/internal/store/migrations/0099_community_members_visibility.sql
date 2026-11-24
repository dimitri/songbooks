-- Who sees a community's Members page (/communities/{id}/members):
-- 'admins', the people whose role grants a management privilege (the
-- ones who see its Admin page), or 'members', every current member.
-- Admins only by default, as before the page existed: a community opens
-- its list to its members by choice.
ALTER TABLE communities ADD COLUMN IF NOT EXISTS members_visibility text NOT NULL DEFAULT 'admins'
  CHECK (members_visibility IN ('admins', 'members'));
