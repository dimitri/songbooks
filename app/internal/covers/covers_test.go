package covers

import (
	"context"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The original album wins over compilations, best-ofs and live albums,
// and another artist's song of the same title doesn't count.
func TestFindITunes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[
		  {"artistName":"Marlena Shaw","trackName":"Mercy, Mercy, Mercy","collectionName":"Chess Soul","collectionArtistName":"Various Artists","artworkUrl100":"https://a/1/100x100bb.jpg","releaseDate":"1967-01-01"},
		  {"artistName":"Marlena Shaw","trackName":"Mercy, Mercy, Mercy (Live)","collectionName":"Live at Montreux","artworkUrl100":"https://a/2/100x100bb.jpg","releaseDate":"1965-01-01"},
		  {"artistName":"Cannonball Adderley","trackName":"Mercy, Mercy, Mercy","collectionName":"Mercy, Mercy, Mercy!","artworkUrl100":"https://a/3/100x100bb.jpg","releaseDate":"1966-01-01"},
		  {"artistName":"Marlena Shaw","trackName":"Mercy, Mercy, Mercy","collectionName":"The Spice of Life","collectionViewUrl":"https://music.apple.com/album/1?uo=4","artworkUrl100":"https://a/4/100x100bb.jpg","releaseDate":"1969-01-01","primaryGenreName":"Soul"}
		]}`))
	}))
	defer srv.Close()
	f := &Finder{ITunes: srv.URL}
	c, ok, err := f.Find(context.Background(), "Mercy, Mercy, Mercy", "Marlena Shaw", "")
	if err != nil || !ok {
		t.Fatalf("not found: %v", err)
	}
	if c.Album != "The Spice of Life" || c.Image != "https://a/4/600x600bb.jpg" || c.Link != "https://music.apple.com/album/1" || c.Genre != "Soul" || c.Year != 1969 {
		t.Errorf("got %+v", c)
	}
}

// Nothing found: the YouTube reference's thumbnail.
func TestFindYouTube(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"results":[],"data":[]}`)) }))
	defer empty.Close()
	f := &Finder{ITunes: empty.URL, Deezer: empty.URL}
	c, ok, _ := f.Find(context.Background(), "Some Song", "Nobody", "abcdefghijk")
	if !ok || c.Source != "youtube" || c.Image != "https://i.ytimg.com/vi/abcdefghijk/hqdefault.jpg" {
		t.Errorf("got %v %+v", ok, c)
	}
	if _, ok, _ := f.Find(context.Background(), "Some Song", "Nobody", ""); ok {
		t.Error("found something in nothing")
	}
}

func TestSameNames(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"Stevie Ray Vaughan & Double Trouble", "Stevie Ray Vaughan", true},
		{"The Beatles", "Beatles", true},
		{"Téléphone", "Telephone", true},
		{"AC/DC", "ACDC", true},
		{"Jeff Beck", "Jeff Buckley", false},
	} {
		if got := sameName(c.a, c.b); got != c.same {
			t.Errorf("sameName(%q, %q) = %v", c.a, c.b, got)
		}
	}
	if !sameTitle("Little Wing (Remastered 2010)", "Little Wing") || !sameTitle("Kiss - Single Version", "Kiss") || sameTitle("Kiss", "Creep") {
		t.Error("sameTitle")
	}
}

// Two thirds red, one third blue: red first, about two thirds.
func TestPalette(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 90, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 90; x++ {
			c := color.RGBA{200, 30, 20, 255}
			if x >= 60 {
				c = color.RGBA{20, 40, 210, 255}
			}
			img.Set(x, y, c)
		}
	}
	p := Palette(img, 4)
	if len(p) == 0 || p[0].Hex != "#c81e14" || p[0].Share < 0.6 || p[0].Share > 0.7 {
		t.Errorf("got %+v", p)
	}
}
