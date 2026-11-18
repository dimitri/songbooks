package main

import (
	"strings"
	"testing"
)

func TestCheckConfirm(t *testing.T) {
	local := "postgres://songbooks:pw@localhost:55432/songbooks?sslmode=disable"
	for _, c := range []struct {
		url, confirm, wantErr string
	}{
		{local, "songbooks@localhost", ""},
		{"postgres://u:p@127.0.0.1/demo", "demo@127.0.0.1", ""},
		{"postgres://songbooks:pw@db:5432/songbooks", "songbooks@db", ""},
		{"host=db dbname=songbooks user=songbooks", "songbooks@db", ""},
		{local, "", "pass --confirm songbooks@localhost"},
		{local, "songbooks@db", "does not match"},
		{local, "other@localhost", "does not match"},
		{"postgres://u:p@app.songbooks.studio/songbooks", "songbooks@app.songbooks.studio", "not a local or staging"},
		{"postgres://u:p@10.0.0.5/songbooks", "songbooks@10.0.0.5", "not a local or staging"},
		{"postgres://u:p@dbserver/songbooks", "songbooks@dbserver", "not a local or staging"},
		{"host=/var/run/postgresql dbname=songbooks", "songbooks@/var/run/postgresql", "not a local or staging"},
		{"", "songbooks@localhost", "no database"},
	} {
		_, _, err := checkConfirm(c.url, c.confirm)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%q --confirm %q: %v", c.url, c.confirm, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%q --confirm %q: got %v, want an error with %q", c.url, c.confirm, err, c.wantErr)
		}
	}
}

func TestRunRefusesWithoutConfirm(t *testing.T) {
	err := run([]string{"anonymize", "--db", "postgres://u:p@localhost/songbooks"})
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("got %v", err)
	}
	err = run([]string{"fixtures", "--db", "postgres://u:p@prod.example.com/songbooks", "--confirm", "songbooks@prod.example.com"})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckDeployment(t *testing.T) {
	env := func(rp string) func(string) string {
		return func(k string) string {
			if k == "SONGBOOKS_WEBAUTHN_RP_ID" {
				return rp
			}
			return ""
		}
	}
	for _, c := range []struct {
		host, rp string
		ok       bool
	}{
		{"db", "songbooks.tapoueh.org", true},
		{"db", "app.songbooks.studio", false},
		{"db", "", false},
		{"localhost", "", true},
		{"localhost", "App.Songbooks.Studio", false},
		{"127.0.0.1", "songbooks.fr", false},
	} {
		if err := checkDeployment(c.host, env(c.rp)); (err == nil) != c.ok {
			t.Errorf("host %s, RP ID %q: got %v", c.host, c.rp, err)
		}
	}
}
