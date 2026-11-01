-- A person can select one or more instruments they play — reusing the
-- jam-signup instrument vocabulary (drummer/bassist/guitarist/singer,
-- plus free-text "other"; see store.DefaultInstrumentLimits). Stored as
-- a plain text array: no fixed enum, since "other" is explicitly
-- free-text and jam instrument limits already treat any string as a
-- valid instrument name.
ALTER TABLE people ADD COLUMN instruments text[] NOT NULL DEFAULT '{}';
