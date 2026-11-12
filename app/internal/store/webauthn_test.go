package store

import (
	"context"
	"testing"
)

// A passkey is named from its AAGUID (or the browser it was added on) with
// no nickname; a pre-0068 passkey with neither gets a browser label from
// its first login, and a label is never overwritten after that.
func TestWebAuthnCredentialNaming(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	personID, err := st.CreatePerson(ctx, "keys", "Keys", "keys@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}

	if _, err := st.AddWebAuthnCredential(ctx, personID, []byte("cred-google"), []byte("pk"), "none", []string{"hybrid", "internal"}, 0,
		true, true, true, true, "ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4", "Chrome on Android"); err != nil {
		t.Fatalf("AddWebAuthnCredential: %v", err)
	}
	// As if added before migration 0068: no AAGUID, no label.
	if _, err := st.db.Exec(ctx, `INSERT INTO webauthn_credentials (person_id, credential_id, public_key) VALUES ($1, 'cred-old', 'pk')`, personID); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateWebAuthnCredentialAfterLogin(ctx, []byte("cred-old"), 0, false, false, true, true, "Safari on Mac"); err != nil {
		t.Fatalf("UpdateWebAuthnCredentialAfterLogin: %v", err)
	}
	if err := st.UpdateWebAuthnCredentialAfterLogin(ctx, []byte("cred-old"), 0, false, false, true, true, "Firefox on Linux"); err != nil {
		t.Fatalf("UpdateWebAuthnCredentialAfterLogin: %v", err)
	}
	if err := st.UpdateWebAuthnCredentialAfterLogin(ctx, []byte("cred-google"), 0, true, true, true, true, "Edge on Windows"); err != nil {
		t.Fatalf("UpdateWebAuthnCredentialAfterLogin: %v", err)
	}

	creds, err := st.ListWebAuthnCredentials(ctx, personID)
	if err != nil {
		t.Fatalf("ListWebAuthnCredentials: %v", err)
	}
	got := map[string]WebAuthnCredential{}
	for _, c := range creds {
		got[string(c.CredentialID)] = c
	}
	if c := got["cred-google"]; c.AAGUID != "ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4" || c.DeviceLabel != "Chrome on Android" || c.Label() != "Google Password Manager" || c.LastUsedAt == nil {
		t.Errorf("new passkey: %+v, Label %q", c, c.Label())
	}
	if c := got["cred-old"]; c.AAGUID != "" || c.Label() != "Safari on Mac" {
		t.Errorf("old passkey: AAGUID %q, Label %q", c.AAGUID, c.Label())
	}

	if err := st.SetWebAuthnCredentialNickname(ctx, personID, got["cred-google"].ID, "Work phone"); err != nil {
		t.Fatal(err)
	}
	c, err := st.GetWebAuthnCredentialByCredentialID(ctx, []byte("cred-google"))
	if err != nil || c.Label() != "Work phone" || c.AutomaticLabel() != "Google Password Manager" {
		t.Errorf("renamed: %v, Label %q", err, c.Label())
	}
}
