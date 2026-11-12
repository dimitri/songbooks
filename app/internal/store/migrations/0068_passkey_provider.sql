-- Name passkeys automatically instead of asking for a nickname.
--
-- aaguid identifies the passkey provider that made the credential (Apple
-- Passwords, Google Password Manager, 1Password, a security key model…);
-- the name is looked up at display time (store/passkey_providers.go), so
-- only the id is kept. NULL for passkeys added before this migration: the
-- AAGUID only travels in the registration response, which was never kept,
-- so those rows can't be backfilled.
--
-- device_label is the fallback when the AAGUID is unknown or all zeros:
-- the browser and device it was added on ("Safari on iPhone"), from the
-- User-Agent. For older rows it is filled on their next passkey login.
-- nickname stays as an optional override the person can set later.
ALTER TABLE webauthn_credentials
  ADD COLUMN aaguid uuid,
  ADD COLUMN device_label text;

-- One older case is recognisable without the AAGUID: not syncable and
-- reached over USB/NFC/Bluetooth only, i.e. a physical security key.
UPDATE webauthn_credentials
   SET device_label = 'Security key'
 WHERE NOT flag_backup_eligible
   AND transports && ARRAY['usb', 'nfc', 'ble']
   AND NOT transports && ARRAY['internal', 'hybrid'];
