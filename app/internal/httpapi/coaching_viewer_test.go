package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/store"
)

// TestSeeAsAndVoiceTypes: who a coaching community's pages are for, and a
// site admin's "See as…": an admin who is a student sees the student's
// pages; only a site admin may look as someone else, a student's preview
// is read-only and shows that student's data, never to anyone else; a
// forged cookie does nothing. And the voice type: the coach's assessment
// and its history, which the student reads too; the type on the cards.
func TestSeeAsAndVoiceTypes(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	person := func(username, name string) int64 {
		t.Helper()
		id, err := st.CreatePerson(ctx, username, name, username+"@example.com")
		must(err)
		return id
	}
	coach := person("coachuser", "Cora Coach")
	stan := person("stanuser", "Stan Student")
	olga := person("olgauser", "Olga Other")
	dim := person("dimuser", "Dim Admin") // a site admin, and a student here
	ada := person("adauser", "Ada Admin") // a site admin, not a member
	must(st.SetAdmin(ctx, dim, true))
	must(st.SetAdmin(ctx, ada, true))
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	must(st.SetCommunityKind(ctx, c, "coaching"))
	for _, p := range []int64{stan, olga, dim} {
		_, err := st.AddCommunityMember(ctx, c, p, "member", coach)
		must(err)
	}
	song, err := st.CreateVariant(ctx, store.OwnerCommunity, c, "Alpha", "", coach)
	must(err)
	other, err := st.CreateVariant(ctx, store.OwnerCommunity, c, "Bravo", "", coach)
	must(err)
	for who, name := range map[int64]string{stan: "STAN-BOOK", olga: "OLGA-BOOK", dim: "DIM-BOOK"} {
		sb, err := st.StudentSongbook(ctx, c, who, name, coach)
		must(err)
		must(st.AddToSongbook(ctx, sb, song, coach))
	}

	tokens := map[int64]string{}
	for _, p := range []int64{coach, stan, olga, dim, ada} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	cookies := map[int64]string{} // the see-as cookie each one holds
	do := func(who int64, method, path, body string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("Accept-Language", "en")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		if v := cookies[who]; v != "" {
			r.AddCookie(&http.Cookie{Name: seeAsCookie, Value: v})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		for _, ck := range w.Result().Cookies() {
			if ck.Name == seeAsCookie {
				cookies[who] = ck.Value
				if ck.MaxAge < 0 {
					cookies[who] = ""
				}
			}
		}
		return w.Code, w.Body.String()
	}
	books := fmt.Sprintf("/communities/%d/songbooks", c)
	seeAs := func(who int64, as string) int {
		t.Helper()
		code, _ := do(who, "GET", fmt.Sprintf("/communities/%d/see-as?as=%s&back=%s", c, as, books), "")
		return code
	}
	studentView := func(body, book string) bool {
		return strings.Contains(body, "Your songbook with") && strings.Contains(body, book)
	}

	// By default: the admin who is a student sees their own student's
	// view; the admin who isn't a member, the coach's; nobody's switch
	// but the admins'.
	_, body := do(dim, "GET", books, "")
	if !studentView(body, "DIM-BOOK") || strings.Contains(body, `class="gig-row cbk-row"`) {
		t.Error("an admin who is a student doesn't see their student view")
	}
	if !strings.Contains(body, `class="see-as`) {
		t.Error("the admin has no See as…")
	}
	_, body = do(ada, "GET", books, "")
	if !strings.Contains(body, `class="gig-row cbk-row"`) {
		t.Error("an admin who isn't a member doesn't see the coach's view")
	}
	_, body = do(stan, "GET", books, "")
	if strings.Contains(body, `class="see-as`) {
		t.Error("a student is offered See as…")
	}

	// A student can't switch, and a cookie made up doesn't count.
	if code := seeAs(stan, fmt.Sprintf("student:%d", olga)); code != http.StatusForbidden {
		t.Errorf("a student's See as…: %d", code)
	}
	cookies[stan] = fmt.Sprintf("%d:student:%d", c, olga)
	_, body = do(stan, "GET", books, "")
	if !studentView(body, "STAN-BOOK") || strings.Contains(body, "OLGA-BOOK") {
		t.Error("a forged cookie shows another student's songbook")
	}
	if code, _ := do(stan, "GET", fmt.Sprintf("/communities/%d/students/%d", c, olga), ""); code != http.StatusNotFound {
		t.Errorf("a forged cookie opens another student's page: %d", code)
	}
	cookies[stan] = fmt.Sprintf("%d:coach", c)
	_, body = do(stan, "GET", books, "")
	if strings.Contains(body, `class="gig-row cbk-row"`) || strings.Contains(body, "OLGA-BOOK") {
		t.Error("a forged coach cookie shows the coach's view")
	}
	if code, _ := do(stan, "PUT", fmt.Sprintf("/api/communities/%d/students/%d/voice", c, olga), `{"voiceType":"soprano"}`); code != http.StatusForbidden {
		t.Errorf("a forged coach cookie sets a voice type: %d", code)
	}
	cookies[stan] = ""

	// The admin looks as Stan: Stan's songbook, his page, not Olga's;
	// read-only; then as the coach, acting as themselves; then leaves.
	if code := seeAs(dim, fmt.Sprintf("student:%d", stan)); code != http.StatusSeeOther {
		t.Fatalf("See as Stan: %d", code)
	}
	_, body = do(dim, "GET", books, "")
	if !studentView(body, "STAN-BOOK") || strings.Contains(body, "DIM-BOOK") || !strings.Contains(body, "Preview: what Stan Student sees.") {
		t.Error("as Stan: not Stan's view, or no preview banner")
	}
	if code, _ := do(dim, "GET", fmt.Sprintf("/communities/%d/students/%d", c, stan), ""); code != http.StatusOK {
		t.Errorf("as Stan, his page: %d", code)
	}
	if code, _ := do(dim, "GET", fmt.Sprintf("/communities/%d/students/%d", c, olga), ""); code != http.StatusNotFound {
		t.Errorf("as Stan, Olga's page: %d", code)
	}
	if code, _ := do(dim, "POST", fmt.Sprintf("/api/communities/%d/song-requests", c), fmt.Sprintf(`{"variantId":%d}`, other)); code != http.StatusConflict {
		t.Errorf("asking during a preview: %d", code)
	}
	if pending, _ := st.PendingSongRequests(ctx, c); len(pending) != 0 {
		t.Errorf("a request was made during a preview: %+v", pending)
	}
	if code := seeAs(dim, "member"); code != http.StatusSeeOther {
		t.Fatalf("See as a member: %d", code)
	}
	_, body = do(dim, "GET", books, "")
	for _, b := range []string{"STAN-BOOK", "OLGA-BOOK", "DIM-BOOK"} {
		if strings.Contains(body, b) {
			t.Errorf("as a member with nothing: %s shown", b)
		}
	}
	if code := seeAs(dim, "coach"); code != http.StatusSeeOther {
		t.Fatalf("See as the coach: %d", code)
	}
	_, body = do(dim, "GET", books, "")
	if !strings.Contains(body, `class="gig-row cbk-row"`) || !strings.Contains(body, "Preview: the coach’s pages.") {
		t.Error("as the coach: not the coach's view")
	}
	voice := fmt.Sprintf("/api/communities/%d/students/%d/voice", c, stan)
	if code, _ := do(dim, "PUT", voice, `{"voiceType":"baritone","note":"by the admin"}`); code != http.StatusNoContent {
		t.Errorf("acting as the coach: %d", code)
	}
	if code := seeAs(dim, ""); code != http.StatusSeeOther {
		t.Fatalf("leaving the preview: %d", code)
	}
	if _, body := do(dim, "GET", books, ""); !studentView(body, "DIM-BOOK") {
		t.Error("after the preview: not the admin's own student view")
	}
	if code, _ := do(dim, "PUT", voice, `{"voiceType":"tenor"}`); code != http.StatusForbidden {
		t.Errorf("an admin who is a student sets a voice type outside the coach's preview: %d", code)
	}

	// The voice type: the coach's, with its history; the student sees the
	// type and range, the assessment once shared; the cards show the type.
	if code, _ := do(coach, "PUT", voice, `{"voiceType":"tenor","tessituraLow":50,"tessituraHigh":64,"note":"THE-ASSESSMENT"}`); code != http.StatusNoContent {
		t.Fatalf("the coach's assessment: %d", code)
	}
	if code, _ := do(olga, "PUT", voice, `{"voiceType":"bass"}`); code != http.StatusForbidden {
		t.Errorf("a student sets another's voice type: %d", code)
	}
	if code, _ := do(stan, "PUT", voice, `{"voiceType":"bass"}`); code != http.StatusForbidden {
		t.Errorf("a student sets their own voice type: %d", code)
	}
	stanPage := fmt.Sprintf("/communities/%d/students/%d", c, stan)
	_, body = do(coach, "GET", stanPage, "")
	for _, want := range []string{"Tenor", "THE-ASSESSMENT", "by the admin", `class="voice-history"`, "Baritone"} {
		if !strings.Contains(body, want) {
			t.Errorf("the coach's page of Stan: no %q", want)
		}
	}
	_, body = do(stan, "GET", stanPage, "")
	if !strings.Contains(body, `<strong class="voice-type-name">Tenor</strong>`) || !strings.Contains(body, "THE-ASSESSMENT") || !strings.Contains(body, `class="voice-history"`) {
		t.Error("Stan's page: no type, or no assessment and history")
	}
	if strings.Contains(body, `class="voice-assess form-stack"`) {
		t.Error("Stan's page: the coach's assessment form")
	}
	// The label as a working opinion, with the descriptors: the range
	// leads, then "leaning tenor · Nov 2026", the timbre, the passages.
	if code, _ := do(coach, "PUT", voice, `{"voiceType":"tenor","leaning":true,"tessituraLow":50,"tessituraHigh":64,"timbre":"warm","passaggioLow":52,"passaggioHigh":64,"note":"THE-ASSESSMENT"}`); code != http.StatusNoContent {
		t.Fatalf("the label leaning: %d", code)
	}
	if code, _ := do(coach, "PUT", voice, `{"timbre":"shiny"}`); code != http.StatusBadRequest {
		t.Errorf("an unknown timbre: %d", code)
	}
	_, body = do(stan, "GET", stanPage, "")
	for _, want := range []string{`<strong class="voice-type-name">leaning tenor</strong>`, "warm timbre", "passages at E3 and E4", "comfortable D3–E4", "Working label"} {
		if !strings.Contains(body, want) {
			t.Errorf("Stan's voice: no %q", want)
		}
	}
	if strings.Index(body, `class="voice-range-line"`) > strings.Index(body, `class="voice-label-line"`) {
		t.Error("the working label comes before the range")
	}

	// Notes: private (the coach's and the staff's) or shared (Stan reads
	// them, and is told). A student never gets a private one: not on his
	// page, not through the API, not in an admin's preview of him, not in
	// search or his calendar feed.
	must(st.SetStudentNotes(ctx, c, stan, "ABOUT-STAN-PRIVATE", coach))
	notes := fmt.Sprintf("/api/communities/%d/students/%d/teaching-notes", c, stan)
	if code, _ := do(coach, "POST", notes, `{"body":"PRIVATE-NOTE"}`); code != http.StatusOK {
		t.Fatalf("a private note: %d", code)
	}
	if code, _ := do(coach, "POST", notes, fmt.Sprintf(`{"body":"SHARED-NOTE","shared":true,"variantId":%d}`, song)); code != http.StatusOK {
		t.Fatalf("a shared note: %d", code)
	}
	if code, _ := do(coach, "POST", notes, `{"body":"x","variantId":999999}`); code != http.StatusBadRequest {
		t.Errorf("a note about a song from elsewhere: %d", code)
	}
	all, err := st.ListTeachingNotes(ctx, c, stan)
	must(err)
	var privateID int64
	for _, n := range all {
		if n.Body == "PRIVATE-NOTE" {
			privateID = n.ID
		}
	}
	share := fmt.Sprintf("%s/%d/share", notes, privateID)
	for _, who := range []int64{stan, olga} {
		if code, _ := do(who, "POST", notes, `{"body":"mine","shared":true}`); code != http.StatusForbidden {
			t.Errorf("a student writes a note: %d", code)
		}
		if code, _ := do(who, "POST", share, ""); code != http.StatusForbidden {
			t.Errorf("a student shares a private note: %d", code)
		}
		if code, _ := do(who, "PUT", fmt.Sprintf("%s/%d", notes, privateID), `{"body":"changed"}`); code != http.StatusForbidden {
			t.Errorf("a student rewrites a note: %d", code)
		}
	}
	noPrivate := func(who string, body string) {
		t.Helper()
		for _, secret := range []string{"PRIVATE-NOTE", "ABOUT-STAN-PRIVATE"} {
			if strings.Contains(body, secret) {
				t.Errorf("%s: %s shown", who, secret)
			}
		}
	}
	_, body = do(coach, "GET", stanPage, "")
	if !strings.Contains(body, "PRIVATE-NOTE") || !strings.Contains(body, "SHARED-NOTE") {
		t.Error("the coach doesn't see both notes")
	}
	// Stan's overview says so, until he opens his page.
	overview := fmt.Sprintf("/communities/%d", c)
	_, body = do(stan, "GET", overview, "")
	if !strings.Contains(body, `class="coach-new-note"`) || !strings.Contains(body, "New note from Cora") {
		t.Error("Stan's overview doesn't say a note is new")
	}
	noPrivate("Stan's overview", body)
	// An admin's preview of Stan: his shared notes, nothing private, and
	// his "new" left as it is.
	seeAs(dim, fmt.Sprintf("student:%d", stan))
	_, body = do(dim, "GET", stanPage, "")
	if !strings.Contains(body, "SHARED-NOTE") {
		t.Error("the preview of Stan: no shared note")
	}
	noPrivate("the preview of Stan", body)
	if code, _ := do(dim, "POST", share, ""); code != http.StatusConflict {
		t.Errorf("sharing during a preview of Stan: %d", code)
	}
	seeAs(dim, "")
	if n, _, _ := st.UnreadNotificationsAt(ctx, stan, "teaching_note_shared", sharedNotesLink(c, stan)); n != 1 {
		t.Errorf("the preview marked Stan's note read: %d unread", n)
	}
	_, body = do(stan, "GET", stanPage, "")
	if !strings.Contains(body, "SHARED-NOTE") || !strings.Contains(body, `id="notes"`) {
		t.Error("Stan's page: no shared note")
	}
	noPrivate("Stan's page", body)
	if _, body := do(stan, "GET", overview, ""); strings.Contains(body, `class="coach-new-note"`) {
		t.Error("Stan's overview still says new after he read it")
	}
	if code, _ := do(olga, "GET", stanPage, ""); code != http.StatusNotFound {
		t.Errorf("Olga opens Stan's page: %d", code)
	}
	_, body = do(stan, "GET", "/api/search?q=PRIVATE", "")
	noPrivate("Stan's search", body)
	tok, err := st.CalendarFeedToken(ctx, stan)
	must(err)
	_, body = do(stan, "GET", "/calendar/feed/"+tok+".ics", "")
	noPrivate("Stan's calendar feed", body)
	// Shared, the private note: Stan reads it and is told; once only.
	if code, _ := do(coach, "POST", share, ""); code != http.StatusOK {
		t.Fatalf("sharing the private note: %d", code)
	}
	if code, _ := do(coach, "POST", share, ""); code != http.StatusNotFound {
		t.Errorf("sharing it twice: %d", code)
	}
	if _, body := do(stan, "GET", stanPage, ""); !strings.Contains(body, "PRIVATE-NOTE") {
		t.Error("the note shared isn't on Stan's page")
	}
	inbox, _ := st.ListNotifications(ctx, stan, false)
	told := 0
	for _, n := range inbox {
		if n.Kind == "teaching_note_shared" {
			told++
		}
	}
	if told != 2 {
		t.Errorf("Stan was told %d times, want 2", told)
	}
}
