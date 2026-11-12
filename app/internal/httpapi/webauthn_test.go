package httpapi

import (
	"testing"

	"app/internal/store"
)

func TestPasskeyDeviceLabel(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15":                          "Safari on Mac",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1":        "Safari on iPhone",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/131.0.6778.73 Mobile/15E148 Safari/604.1": "Chrome on iPhone",
		"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36":                                "Chrome on Android",
		"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/26.0 Chrome/122.0.0.0 Mobile Safari/537.36":     "Samsung Internet on Android",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0":                  "Edge on Windows",
		"Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0":                                                                         "Firefox on Linux",
		"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36":                                 "Chrome on ChromeOS",
		"curl/8.7.1": "",
	}
	for ua, want := range cases {
		if got := passkeyDeviceLabel(ua); got != want {
			t.Errorf("passkeyDeviceLabel(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestPasskeyProviderLabel(t *testing.T) {
	// Google Password Manager's AAGUID, as raw bytes from the authenticator data.
	raw := []byte{0xea, 0x9b, 0x8d, 0x66, 0x4d, 0x01, 0x1d, 0x21, 0x3c, 0xe4, 0xb6, 0xb4, 0x8c, 0xb5, 0x75, 0xd4}
	aaguid := aaguidString(raw)
	if aaguid != "ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4" {
		t.Fatalf("aaguidString = %q", aaguid)
	}
	if got := (store.WebAuthnCredential{AAGUID: aaguid, DeviceLabel: "Chrome on Android"}).Label(); got != "Google Password Manager" {
		t.Errorf("known AAGUID: Label() = %q", got)
	}
	if got := (store.WebAuthnCredential{AAGUID: "00000000-0000-0000-0000-000000000000", DeviceLabel: "Safari on Mac"}).Label(); got != "Safari on Mac" {
		t.Errorf("zero AAGUID: Label() = %q", got)
	}
	if got := (store.WebAuthnCredential{AAGUID: aaguid, Nickname: "Work phone"}).Label(); got != "Work phone" {
		t.Errorf("nickname: Label() = %q", got)
	}
	if got := (store.WebAuthnCredential{}).Label(); got != "Passkey" {
		t.Errorf("nothing known: Label() = %q", got)
	}
	if aaguidString(nil) != "" {
		t.Error("aaguidString(nil) should be empty")
	}
}
