package main

import "testing"

func TestRedactURL(t *testing.T) {
	for in, want := range map[string]string{
		"postgres://songbooks:secret@db:5432/songbooks?sslmode=disable": "postgres://songbooks:xxxxx@db:5432/songbooks?sslmode=disable",
		"postgres://localhost/songbooks":                                "postgres://localhost/songbooks",
	} {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q) = %q", in, got)
		}
	}
}
