package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/manasnikam/skinverdict/internal/app"
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
	for path, want := range map[string]int{"/files/scans/x/selfie.jpg": 200, "/files/skinverdict.db": 404, "/files/scans/../skinverdict.db": 404} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want && !(want == 404 && rec.Code >= 300 && rec.Code < 500) {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}
