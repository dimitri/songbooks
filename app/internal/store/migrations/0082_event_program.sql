-- A coaching community's event program (an open mic, the end-of-year
-- concert): its songs in running order, each in the key it will be sung
-- in, and who sings it, lead or backing vocals (docs/coaching.md).
CREATE TABLE event_program (
  id bigserial PRIMARY KEY,
  event_id bigint NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  position int NOT NULL,
  variant_id bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  semitones int NOT NULL DEFAULT 0,
  UNIQUE (event_id, variant_id)
);
CREATE INDEX event_program_event_idx ON event_program (event_id, position);

CREATE TABLE event_program_singers (
  program_id bigint NOT NULL REFERENCES event_program(id) ON DELETE CASCADE,
  person_id bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  role text NOT NULL CHECK (role IN ('lead', 'backing')),
  PRIMARY KEY (program_id, person_id)
);
-- One lead per song.
CREATE UNIQUE INDEX event_program_one_lead ON event_program_singers (program_id) WHERE role = 'lead';
