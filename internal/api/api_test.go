package api

import (
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
