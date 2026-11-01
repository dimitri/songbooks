-- Passkey (WebAuthn) credentials: a THIRD first-class login option
-- alongside password (people.password_hash) and magic-link (login_tokens),
-- additive only — neither existing path is touched. A person may register
-- any number of passkeys (one per device/authenticator); each is this
-- table's own row, scoped to person_id.
--
-- credential_id is the authenticator-assigned credential identifier (the
-- WebAuthn spec's "credential record id") — globally unique, hence the
-- UNIQUE constraint, which is also what a discoverable ("passwordless")
-- login looks up by. public_key is the credential's public key only; there
-- is no private key material on this side of the protocol by design — the
-- private key never leaves the authenticator.
--
-- The four flag_* columns persist the authenticator-data flags recorded at
-- registration (BE/BS/UP/UV in WebAuthn terms). go-webauthn's own login
-- validation (ValidateLogin/ValidatePasskeyLogin) requires the credential's
-- stored BackupEligible flag to still match what the authenticator reports
-- on every subsequent login — "this should NEVER change" per the library's
-- own doc comment on CredentialFlags.BackupEligible — and rejects the
-- ceremony otherwise, so these must be persisted verbatim from the
-- registration response, not left at their zero value.
--
-- sign_count is the clone-detection counter: incremented by the
-- authenticator on each use, checked by go-webauthn's
-- Authenticator.UpdateCounter against the stored value on every login (see
-- httpapi/webauthn.go). Many platform authenticators (e.g. iCloud Keychain/
-- Touch ID synced passkeys) always report 0 and never increment it — the
-- library treats 0-vs-0 as "this authenticator doesn't support counters",
-- not a clone signal, which is why sign_count isn't itself enough to
-- reject a login outright.
CREATE TABLE webauthn_credentials (
  id                   bigserial PRIMARY KEY,
  person_id            bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  credential_id        bytea NOT NULL UNIQUE,
  public_key           bytea NOT NULL,
  attestation_type     text NOT NULL DEFAULT '',
  transports           text[] NOT NULL DEFAULT '{}',
  sign_count           bigint NOT NULL DEFAULT 0,
  flag_backup_eligible boolean NOT NULL DEFAULT false,
  flag_backup_state    boolean NOT NULL DEFAULT false,
  flag_user_present    boolean NOT NULL DEFAULT false,
  flag_user_verified   boolean NOT NULL DEFAULT false,
  created_at           timestamptz NOT NULL DEFAULT now(),
  last_used_at         timestamptz,
  nickname             text
);
CREATE INDEX webauthn_credentials_person_idx ON webauthn_credentials(person_id);
