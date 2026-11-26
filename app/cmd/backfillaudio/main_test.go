package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/store"
)

// scratchStore is a database of its own on the test server.
func scratchStore(t *testing.T) (*store.Store, *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	connString := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if connString == "" {
		connString = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, connString)
	if err != nil {
		t.Skipf("no test database available at %s: %v", connString, err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	b := make([]byte, 6)
	rand.Read(b)
	name := "songbooks_backfill_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("can't make a scratch database: %v", err)
	}
	t.Cleanup(func() { admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") })
	u, err := url.Parse(connString)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	st, err := store.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return st, conn
}

// The dry run (the default) counts and queues nothing; --apply queues.
func TestBackfillDryRun(t *testing.T) {
	st, sql := scratchStore(t)
	ctx := context.Background()
	owner, err := st.CreatePerson(ctx, "Bf Owner", "bf@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"https://www.youtube.com/watch?v=AAAAAAAAAAA", "https://youtu.be/BBBBBBBBBBB", "https://example.com/page"} {
		id, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Song", "", owner)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.SaveVariantByID(ctx, id, "Song", "", link, "title: Song\nreference: "+link+"\n", "", owner); err != nil {
			t.Fatal(err)
		}
	}
	// Songs from before the automatic fetch: no job.
	if _, err := sql.Exec(ctx, `DELETE FROM song_youtube_audio_jobs`); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(ctx, st, &out, false, 0, 0, time.Second); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"songs with a YouTube video as their reference: 2", "to queue: 2", "dry run: nothing queued"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q not in:\n%s", want, out.String())
		}
	}
	var n int
	if err := sql.QueryRow(ctx, `SELECT count(*) FROM song_youtube_audio_jobs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the dry run queued %d (%v)", n, err)
	}
	// --apply --limit 1: one queued, waited on (no worker here: it stays
	// queued until --wait), the next left.
	out.Reset()
	if err := run(ctx, st, &out, true, 1, 0, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := sql.QueryRow(ctx, `SELECT count(*) FROM song_youtube_audio_jobs WHERE status = 'queued' AND trigger = 'backfill'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("--apply --limit 1 queued %d (%v):\n%s", n, err, out.String())
	}
}
