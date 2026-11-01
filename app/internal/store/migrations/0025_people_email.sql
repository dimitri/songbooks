-- Magic-link login (Phase: auth rework) needs an email address to send
-- the link to. Nullable: existing accounts (e.g. the "tapoueh" bootstrap
-- row from store.ImportTree, created with no email on file) keep working
-- via the legacy password login until an admin backfills their email
-- with `songbooks --set-email <username> <email>`. Unique so a lookup by
-- email is unambiguous.
ALTER TABLE people ADD COLUMN email text UNIQUE;
