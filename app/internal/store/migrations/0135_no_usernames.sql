-- Email is the identity (docs/accounts.md): an account is its email,
-- and people are shown by their display name. people.username (0001)
-- was the login name, also shown as "@name" and in member lists; the
-- app no longer reads it for identity or display, nor writes it: new
-- accounts and claimed placeholder accounts (0048) leave it NULL.
--
-- The column and the names already in it stay for now (UNIQUE still
-- holds for those; NULLs never collide). Dropping it is a later,
-- separate step, once no tool or dump depends on it.
ALTER TABLE people ALTER COLUMN username DROP NOT NULL;

COMMENT ON COLUMN people.username IS
  'Legacy login name, no longer read or written by the app (0135): email is the identity. To be dropped.';
