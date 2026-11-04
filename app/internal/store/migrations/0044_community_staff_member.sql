-- Finishes the School->Community rename at the data layer: the role
-- names themselves become 'staff'/'member' (matching the user-facing
-- copy already shipped in 0042/0043), not just 'instructor'/'student'
-- displayed as something else. Zero school_members rows in production
-- (confirmed before 0042), so a plain value rename is safe.
ALTER TYPE school_member_role RENAME VALUE 'instructor' TO 'staff';
ALTER TYPE school_member_role RENAME VALUE 'student' TO 'member';
