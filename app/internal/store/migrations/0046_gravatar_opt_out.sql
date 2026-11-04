-- Lets a person suppress the Gravatar-by-email fallback the public
-- profile picture now uses when picture_url is blank — "use gravatar
-- when it exists, with an option to discard it" (direct request).
-- Default false: existing accounts keep today's effective behavior
-- (blank picture_url -> try Gravatar) until someone opts out.
ALTER TABLE people ADD COLUMN gravatar_opt_out boolean NOT NULL DEFAULT false;
