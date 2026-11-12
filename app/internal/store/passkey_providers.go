package store

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// passkey_providers.json maps an authenticator AAGUID to the name of the
// passkey provider that made it ("Apple Passwords", "Google Password
// Manager", "1Password", a YubiKey model…), names only, no icons. Built
// from two public lists meant for exactly this "name the passkeys in
// account settings" use: the FIDO Convenience Metadata Service
// (https://c-mds.fidoalliance.org/) and the community list
// github.com/passkeydeveloper/passkey-authenticator-aaguids (aaguid.json,
// which has no license file and is frozen since 2026-11-01 — its RP
// guidance says to keep a local snapshot; it gets emptied on 2027-01-06).
// On a key both list, the community name wins: it's the shorter,
// consumer-facing one ("Windows Hello" vs "Windows Hello Hardware
// Authenticator"). Refresh (keeps existing entries, so the frozen
// community names survive once that list is empty):
//
//	jq -S -s '.[0] + (.[2]|del(.no)|map_values(.friendlyNames["en-US"] // empty)) + (.[1]|map_values(.name))' passkey_providers.json <(curl -sL https://raw.githubusercontent.com/passkeydeveloper/passkey-authenticator-aaguids/main/aaguid.json) <(curl -sL https://c-mds.fidoalliance.org/) > new.json && mv new.json passkey_providers.json
//
//go:embed passkey_providers.json
var passkeyProvidersJSON []byte

var passkeyProviders = func() map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(passkeyProvidersJSON, &m); err != nil {
		panic("store: bad passkey_providers.json: " + err.Error())
	}
	return m
}()

const zeroAAGUID = "00000000-0000-0000-0000-000000000000"

// PasskeyProviderName returns the provider name for an AAGUID in its
// canonical text form, or "" when unknown — including the all-zero AAGUID,
// which some providers (and browsers, when no attestation is asked for)
// send on purpose to stay anonymous. Display only: without attestation the
// AAGUID is self-asserted, so it is never a security signal here.
func PasskeyProviderName(aaguid string) string {
	aaguid = strings.ToLower(aaguid)
	if aaguid == "" || aaguid == zeroAAGUID {
		return ""
	}
	return passkeyProviders[aaguid]
}

// ProviderName is the passkey provider resolved from the stored AAGUID at
// render time (not saved with the row), so a refreshed list names older
// passkeys too, with no migration.
func (c WebAuthnCredential) ProviderName() string { return PasskeyProviderName(c.AAGUID) }

// Label is what the profile list and the removal confirm call this passkey:
// the person's own name for it if they gave one, else AutomaticLabel.
func (c WebAuthnCredential) Label() string {
	if c.Nickname != "" {
		return c.Nickname
	}
	return c.AutomaticLabel()
}

// AutomaticLabel is the name nobody had to type: the provider, else the
// browser and device it was added (or, for passkeys from before 0068,
// first used) on, else a generic word — the created/last-used dates next
// to it tell two of those apart.
func (c WebAuthnCredential) AutomaticLabel() string {
	if name := c.ProviderName(); name != "" {
		return name
	}
	if c.DeviceLabel != "" {
		return c.DeviceLabel
	}
	return "Passkey"
}

// CredentialIDBase64URL is the credential id as unpadded base64url, the
// form the browser's Signal API (PublicKeyCredential.signalAllAccepted
// Credentials) expects.
func (c WebAuthnCredential) CredentialIDBase64URL() string {
	return base64.RawURLEncoding.EncodeToString(c.CredentialID)
}
