package store

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestMatchFilesToSongs(t *testing.T) {
	songs := []matchSong{
		{ID: 1, Title: "Mon premier Blues 1", GPName: "01_Mon premier Blues 1.gp", GPSHAs: []string{"a"}},
		{ID: 2, Title: "Mon premier Blues 2", GPSHAs: []string{"b"}},
		{ID: 3, Title: "Plan psychédélique 1", GPSHAs: []string{"c"}},
		{ID: 4, Title: "Plan psychédélique 2", GPSHAs: []string{"d"}},
		{ID: 5, Title: "Rythmique swing avec des accords de neuvième"},
		{ID: 6, Title: "Deux rythmiques à la Jimi Hendrix", GPSHAs: []string{"e"}},
		{ID: 7, Title: "Butterscotch", GPSHAs: []string{"old", "f"}},
	}
	entries := []matchEntry{
		{Dir: "01_Mon premier Blues 1 et 2", Name: "01_Mon premier Blues 1.gp", SHA: "a"},
		{Dir: "01_Mon premier Blues 1 et 2", Name: "01_ Mon premier Blues 2.gp", SHA: "b"},
		{Name: "32_ Plan psychédélique 2.gp", SHA: "d"},
		{Name: "32_ Plan psychédélique 1.gp", SHA: "c"},
		{Name: "32_ Plan psychédélique PB.mp3"},
		{Dir: "06_Rythmique swing", Name: "06_Rythmique swing avec des accords de neuvième PB.gp.wav"},
		{Dir: "09_Deux rythmiques", Name: "09_Deux rythmiques a la Jimi Hendrix 1.gp", SHA: "e"},
		{Dir: "09_Deux rythmiques", Name: "09_Deux rythmiques a la Jimi Hendrix 2.gp", SHA: "zz"},
		{Name: "19_Butterscotch - Freddie King.gp", SHA: "old"},
		{Name: "19_Butterscotch PB.mp3"},
		{Name: "20_Same old Blues.pdf"},
		{Name: "La méthode.pdf"},
	}
	want := []int64{1, 2, 4, 3, 3, 5, 6, 6, 7, 7, 0, 0}
	got := matchFilesToSongs(entries, songs)
	for i, w := range want {
		var g int64
		if got[i] != nil {
			g = *got[i]
		}
		if g != w {
			t.Errorf("%s: song %d, want %d", entries[i].Name, g, w)
		}
	}
}

func TestSongbookFilesAndZip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "me", "Me", "me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	sb, err := st.GetDefaultSongbook(ctx, "person", me)
	if err != nil {
		t.Fatal(err)
	}
	big := bytes.Repeat([]byte("0123456789abcdef"), FileChunkSize/8+3) // two chunks and a bit
	when := time.Date(2024, 9, 30, 15, 37, 0, 0, time.UTC)
	add := func(folder, name string, data []byte) (int64, string) {
		t.Helper()
		id, status, err := st.AddSongbookFile(ctx, NewSongbookFile{SongbookID: sb, Path: folder, Filename: name, ModifiedAt: when, CreatedByID: me}, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return id, status
	}
	add("", "La méthode.pdf", big)
	add("01_Leçon", "01_Leçon PB.mp3", []byte("same bytes"))
	add("19_Other", "19_Other PB.mp3", []byte("same bytes")) // stored once, listed twice
	if _, status := add("01_Leçon", "01_Leçon PB.mp3", []byte("same bytes")); status != FileUnchanged {
		t.Errorf("same file again: %s", status)
	}
	if _, status := add("01_Leçon", "01_Leçon PB.mp3", []byte("new bytes")); status != FileReplaced {
		t.Errorf("new content: %s", status)
	}
	var blobs int
	st.db.QueryRow(ctx, `SELECT count(*) FROM file_blobs`).Scan(&blobs)
	if blobs != 3 {
		t.Errorf("%d blobs, want 3", blobs)
	}

	files, err := st.ListSongbookFiles(ctx, []int64{sb})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[0].Kind != "book" {
		t.Fatalf("files: %+v", files)
	}
	zs, err := st.NewZipStream(ctx, files, "")
	if err != nil {
		t.Fatal(err)
	}
	all, err := io.ReadAll(zs)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(all)) != zs.Size() {
		t.Fatalf("read %d bytes of %d", len(all), zs.Size())
	}
	zr, err := zip.NewReader(bytes.NewReader(all), int64(len(all)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc) // checks the CRC
		rc.Close()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if !f.Modified.Equal(when) {
			t.Errorf("%s: modified %v", f.Name, f.Modified)
		}
		got[f.Name] = string(b)
	}
	if got["01_Leçon/01_Leçon PB.mp3"] != "new bytes" || got["19_Other/19_Other PB.mp3"] != "same bytes" || got["La méthode.pdf"] != string(big) {
		t.Errorf("zip holds %d files: %v", len(got), func() []string {
			var n []string
			for k := range got {
				n = append(n, k)
			}
			return n
		}())
	}

	// A range from the middle, across a file's chunks.
	if _, err := zs.Seek(int64(len(all))/2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	part := make([]byte, FileChunkSize+100)
	if _, err := io.ReadFull(zs, part); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(part, all[len(all)/2:len(all)/2+len(part)]) {
		t.Error("ranged read differs")
	}

	// A folder's zip has its files at the top.
	zs, err = st.NewZipStream(ctx, files[1:2], "01_Leçon")
	if err != nil {
		t.Fatal(err)
	}
	all, _ = io.ReadAll(zs)
	zr, err = zip.NewReader(bytes.NewReader(all), int64(len(all)))
	if err != nil || len(zr.File) != 1 || strings.Contains(zr.File[0].Name, "/") {
		t.Errorf("folder zip: %v %v", err, zr)
	}

	for _, f := range files {
		if err := st.DeleteSongbookFile(ctx, f.ID); err != nil {
			t.Fatal(err)
		}
	}
	st.db.QueryRow(ctx, `SELECT count(*) FROM file_blobs`).Scan(&blobs)
	if blobs != 0 {
		t.Errorf("%d blobs left", blobs)
	}
}
