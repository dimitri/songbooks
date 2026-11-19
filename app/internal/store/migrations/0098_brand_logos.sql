-- brand_logos: a community's or band's uploaded logo (internal/brand,
-- PrepareLogo), stored as bytea like song_audio and file_blobs, and
-- served from /brand-logos/{kind}/{id}/{hash}.{ext} with a year-long
-- cache: the hash is the image's own, so a new logo is a new address.
-- One per owner; deleting the community or band deletes it. The image is
-- always one the server re-encoded (PNG or JPEG, at most 512px a side),
-- never the uploaded file itself.
CREATE TABLE brand_logos (
  id                   bigserial PRIMARY KEY,
  community_id         bigint UNIQUE REFERENCES communities(id) ON DELETE CASCADE,
  band_id              bigint UNIQUE REFERENCES bands(id) ON DELETE CASCADE,
  content_type         text NOT NULL,
  width                int NOT NULL,
  height               int NOT NULL,
  byte_size            int NOT NULL,
  sha256               text NOT NULL,
  data                 bytea NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  CHECK ((community_id IS NULL) <> (band_id IS NULL))
);
