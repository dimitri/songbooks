-- A site-wide admin flag, separate from per-project owner/editor/viewer
-- roles (project_members.role) — admin is "can see/manage the whole
-- instance" (every person, every project), not "has elevated access to
-- one project." See httpapi.requireAdmin, which is a brand-new, separate
-- middleware from requireRole for exactly this reason.
ALTER TABLE people ADD COLUMN is_admin boolean NOT NULL DEFAULT false;
