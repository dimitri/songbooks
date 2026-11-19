package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"app/internal/store"
)

func TestGaleneToken(t *testing.T) {
	key, err := ParseGaleneKey("YRile3tjrURKSwrn0qx64WsTqTWcv67gYb1trDkzAxQ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseGaleneKey("c2hvcnQ"); err == nil {
		t.Fatal("a short key passed")
	}
	g := GaleneConfig{URL: "https://live.example.org", Key: key, KID: "songbooks-1"}
	aud := g.roomURL("0b9f3c2e-0000-4000-8000-000000000001")
	if aud != "https://live.example.org/group/lessons/0b9f3c2e-0000-4000-8000-000000000001/" {
		t.Fatalf("room %q", aud)
	}
	now := time.Unix(1_800_000_000, 0)
	tok, err := g.token(aud, "Émma", studentPermissions, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token %q", tok)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != parts[2] {
		t.Fatal("bad signature")
	}
	var header map[string]string
	var claims struct {
		Sub         string   `json:"sub"`
		Aud         string   `json:"aud"`
		Permissions []string `json:"permissions"`
		Iat, Exp    int64
	}
	h, _ := base64.RawURLEncoding.DecodeString(parts[0])
	c, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if json.Unmarshal(h, &header) != nil || json.Unmarshal(c, &claims) != nil {
		t.Fatal("undecodable token")
	}
	if header["alg"] != "HS256" || header["kid"] != "songbooks-1" {
		t.Fatalf("header %v", header)
	}
	if claims.Sub != "Émma" || claims.Aud != aud || strings.Join(claims.Permissions, ",") != "present,message" || claims.Exp-claims.Iat != 120 {
		t.Fatalf("claims %+v", claims)
	}
}

func TestLiveWindow(t *testing.T) {
	start := time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	ev := store.Event{StartsAt: start, EndsAt: &end, LessonMode: "online"}
	for at, open := range map[time.Duration]bool{-16 * time.Minute: false, -15 * time.Minute: true, 30 * time.Minute: true, 90 * time.Minute: true, 91 * time.Minute: false} {
		if ev.LiveOpen(start.Add(at)) != open {
			t.Errorf("at %v: open %v", at, !open)
		}
	}
	ev.LessonMode = "in_person"
	if ev.LiveOpen(start) {
		t.Error("a lesson in the room is open online")
	}
}

func TestFeedJoinLink(t *testing.T) {
	items := []store.CalendarItem{
		{Kind: "lesson", Title: "Lesson", Starts: time.Now(), Link: "/communities/7/lessons", LessonMode: "online", EventID: 58, UID: "event-58"},
		{Kind: "lesson", Title: "Lesson", Starts: time.Now(), Link: "/communities/7/lessons", LessonMode: "in_person", EventID: 59, UID: "event-59"},
	}
	feed := strings.ReplaceAll(buildFeed(items, "https://app.example.org", "tok/en", time.Now()), "\r\n ", "")
	if !strings.Contains(feed, "URL:https://app.example.org/communities/7/lessons/58/join?f=tok%2Fen") {
		t.Errorf("no join link:\n%s", feed)
	}
	if strings.Contains(feed, "/59/join") {
		t.Error("a lesson in the room has a join link")
	}
}
