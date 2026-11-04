-- group_invite_slots.status: the band status ('core'/'guest') a roster
-- slot grants on claim. Previously every claim hardcoded 'guest'
-- (grantGroupInviteMembershipTx); the roster UI now lets the organizer
-- pick a role per named slot up front, same as a direct add already
-- could — so the group invite link should actually grant it.
ALTER TABLE group_invite_slots ADD COLUMN status text NOT NULL DEFAULT 'guest'
  CHECK (status IN ('core', 'guest'));
