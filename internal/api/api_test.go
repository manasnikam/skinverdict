package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manasnikam/skinverdict/internal/app"
	"github.com/manasnikam/skinverdict/internal/store"
)

func TestFileURL(t *testing.T) {
	for _, c := range []struct{ dir, path, want string }{
		{"./data-local", "data-local/scans/x/pore.jpg", "/files/scans/x/pore.jpg"}, // relative dir with "./" (local default)
		{"/opt/sv/data", "/opt/sv/data/scans/x/selfie.jpg", "/files/scans/x/selfie.jpg"},
		{"./data-local", "/etc/passwd", ""},
		{"./data-local", "", ""},
	} {
		s := &Server{App: &app.App{DataDir: c.dir}}
		if got := s.fileURL(c.path); got != c.want {
			t.Errorf("fileURL(%q, %q) = %q, want %q", c.dir, c.path, got, c.want)
		}
	}
}

func TestFilesServesOnlyScans(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "scans", "x"), 0o755)
	os.WriteFile(filepath.Join(dir, "scans", "x", "selfie.jpg"), []byte("img"), 0o644)
	os.WriteFile(filepath.Join(dir, "skinverdict.db"), []byte("db"), 0o644)
	mux := http.NewServeMux()
	(&Server{App: &app.App{DataDir: dir}}).Routes(mux)
	for path, want := range map[string]int{"/files/scans/x/selfie.jpg": 200, "/files/skinverdict.db": 404, "/files/scans/../skinverdict.db": 404,
		"/files/scans/": 404, "/files/scans/x/": 404, "/files/scans/x": 404} { // no directory listings
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want && !(want == 404 && rec.Code >= 300 && rec.Code < 500) {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestAbuseGuards(t *testing.T) {
	st, err := store.OpenJSON(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	victim := &store.Persona{Name: "victim"}
	st.UpsertPersona(victim)
	s := &Server{App: &app.App{Store: st, DataDir: t.TempDir()}, AdminToken: "s3cret", ScansPerHour: 2}
	mux := http.NewServeMux()
	s.Routes(mux)
	do := func(method, path, auth string, body io.Reader) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, body)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// admin route needs the bearer token
	for auth, want := range map[string]int{"": 401, "Bearer nope": 401, "Bearer s3cret": 200} {
		if got := do("POST", "/api/admin/checkin-all", auth, nil).Code; got != want {
			t.Errorf("checkin-all with %q = %d, want %d", auth, got, want)
		}
	}
	s.AdminToken = ""
	if got := do("POST", "/api/admin/checkin-all", "Bearer ", nil).Code; got != 404 {
		t.Errorf("checkin-all with no token configured = %d, want 404", got)
	}

	// create can't overwrite an existing persona or set its profile
	do("POST", "/api/personas", "", strings.NewReader(`{"id":"`+victim.ID+`","name":"pwned","profile":"oily"}`))
	if p, _ := st.Persona(victim.ID); p.Name != "victim" || p.Profile != "" {
		t.Errorf("persona overwritten: %+v", p)
	}

	// oversized uploads are refused, raw and multipart
	big := bytes.Repeat([]byte("x"), 14<<20)
	if got := do("POST", "/api/trials/x/scans", "", bytes.NewReader(big)).Code; got != 413 {
		t.Errorf("raw 14MB scan = %d, want 413", got)
	}
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	fw, _ := mw.CreateFormFile("image", "selfie.jpg")
	fw.Write(big)
	mw.Close()
	req := httptest.NewRequest("POST", "/api/trials/x/scans", &mp)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Errorf("multipart 14MB scan = %d, want 413", rec.Code)
	}

	// hourly scan budget
	if !s.takeScan() || !s.takeScan() || s.takeScan() {
		t.Error("budget of 2 should allow exactly 2 scans")
	}
	s.windowStart = s.windowStart.Add(-time.Hour)
	if !s.takeScan() {
		t.Error("budget should reset after an hour")
	}
}
