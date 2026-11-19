-- "Je joue": the instruments a person plays, as the song player knows them
-- (store/player_parts.go, httpapi/player_parts.go). The player shows one
-- level per part they play (0 takes it out of the recording, 100 the
-- recording as it is), from the recording's parts (cmd/stemsd).
--
-- By default it is their profile's instruments (people.instruments,
-- normalised by internal/instruments). A row here overrides that: for one
-- context (a band, a community; context_kind '' and no id for "elsewhere"),
-- and either for all its songs (variant_id NULL) or for one song. Guitar
-- with the band, voice in the coaching community; voice and piano for that
-- one song. instruments are canonical IDs (internal/instruments); an empty
-- list is "nothing: I listen".
CREATE TABLE player_parts (
    person_id    bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    context_kind text   NOT NULL DEFAULT '' CHECK (context_kind IN ('', 'band', 'community')),
    context_id   bigint CHECK ((context_kind = '') = (context_id IS NULL)),
    variant_id   bigint REFERENCES songs(id) ON DELETE CASCADE,
    instruments  text[] NOT NULL DEFAULT '{}',
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (person_id, context_kind, context_id, variant_id)
);
