package store

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/covers"
)

// A songbook's songs get their covers once: found ones keep their image,
// album and colors, the others are marked looked for; again looks only
// at those without an album cover.
func TestFillCovers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// Works outlive a test's reset (newTestStore), and their covers with them.
	if _, err := st.db.Exec(ctx, `DELETE FROM work_covers`); err != nil {
		t.Fatal(err)
	}
	me, err := st.CreatePerson(ctx, "Listener", "listener@example.com")
	if err != nil {
		t.Fatal(err)
	}
	sb, err := st.GetDefaultSongbook(ctx, "person", me)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateVariant(ctx, OwnerPerson, me, "Valerie", "Amy Winehouse", me); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateVariant(ctx, OwnerPerson, me, "Unknown Song", "Nobody", me); err != nil {
		t.Fatal(err)
	}

	searches := 0
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		searches++
		if strings.Contains(r.URL.Query().Get("term"), "Valerie") {
			fmt.Fprintf(w, `{"results":[{"artistName":"Amy Winehouse","trackName":"Valerie","collectionName":"Back to Black","artworkUrl100":"%s/art/100x100bb.png","releaseDate":"2006-10-27","primaryGenreName":"Soul"}]}`, base)
			return
		}
		w.Write([]byte(`{"results":[],"data":[]}`))
	})
	mux.HandleFunc("/art/", func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 20, 20))
		for y := range 20 {
			for x := range 20 {
				img.Set(x, y, color.RGBA{180, 30, 40, 255})
			}
		}
		png.Encode(w, img)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL
	f := &covers.Finder{ITunes: srv.URL, Deezer: srv.URL}

	found, missing, err := st.FillCovers(ctx, f, sb, false, 0, nil)
	if err != nil || found != 1 || missing != 1 {
		t.Fatalf("fill: %d found, %d missing, %v", found, missing, err)
	}
	songs, err := st.SongbookCovers(ctx, sb)
	if err != nil || len(songs) != 2 {
		t.Fatalf("covers: %+v %v", songs, err)
	}
	for _, sc := range songs {
		if !sc.Looked {
			t.Errorf("%s not marked looked for", sc.Title)
		}
		switch sc.Title {
		case "Valerie":
			if sc.Cover == nil || sc.Cover.Album != "Back to Black" || sc.Cover.Image != srv.URL+"/art/600x600bb.png" || len(sc.Cover.Colors) == 0 || sc.Cover.Colors[0].Hex != "#b41e28" {
				t.Errorf("Valerie: %+v", sc.Cover)
			}
		default:
			if sc.Cover != nil {
				t.Errorf("%s: %+v", sc.Title, sc.Cover)
			}
		}
	}

	// Nothing new to look for; again asks only about the song without one.
	searches = 0
	if found, missing, _ := st.FillCovers(ctx, f, sb, false, 0, nil); found+missing != 0 || searches != 0 {
		t.Errorf("looked again: %d %d, %d searches", found, missing, searches)
	}
	if found, missing, _ := st.FillCovers(ctx, f, sb, true, 0, nil); found != 0 || missing != 1 {
		t.Errorf("again: %d found, %d missing", found, missing)
	}

	look := SongbookLook(songs)
	if look.Found != 1 || look.Songs != 2 || len(look.Mosaic) != 1 || look.Accent == "" {
		t.Errorf("look: %+v", look)
	}
}
