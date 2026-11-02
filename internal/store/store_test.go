package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

func conformance(t *testing.T, s Store) {
	t.Helper()
	p := &Persona{Name: "Test"}
	if err := s.UpsertPersona(p); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Persona(p.ID); err != nil || got.Name != "Test" {
		t.Fatalf("persona: %v %+v", err, got)
	}
	tr := &Trial{PersonaID: p.ID, ProductID: "nia10", Target: "pore", Concerns: []string{"pore", "redness"}, NextCheck: time.Now().Add(time.Hour)}
	if err := s.CreateTrial(tr); err != nil {
		t.Fatal(err)
	}
	for i, d := range []int{0, 7} {
		sc := &Scan{TrialID: tr.ID, At: time.Now().AddDate(0, 0, d), Scores: map[string]float64{"pore": 20 + float64(i)*5}, UIScores: map[string]float64{"pore": 50}, MaskPaths: map[string]string{"pore": "x.jpg"}, RunMS: 2500, Units: 9}
		if err := s.AddScan(sc); err != nil {
			t.Fatal(err)
		}
	}
	scans, err := s.Scans(tr.ID)
	if err != nil || len(scans) != 2 || scans[1].Scores["pore"] != 25 || scans[1].MaskPaths["pore"] != "x.jpg" {
		t.Fatalf("scans: %v %+v", err, scans)
	}
	tr.Status = "reordered"
	tr.LastAction = &Action{Kind: "reorder", Reason: "r", At: time.Now()}
	if err := s.UpdateTrial(tr); err != nil {
		t.Fatal(err)
	}
	got, err := s.Trial(tr.ID)
	if err != nil || got.Status != "reordered" || got.LastAction == nil || got.LastAction.Kind != "reorder" || len(got.Concerns) != 2 {
		t.Fatalf("trial: %v %+v", err, got)
	}
	if _, err := s.Trial("nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if ts, _ := s.Trials(); len(ts) < 1 {
		t.Fatal("trials empty")
	}
}

func TestJSON(t *testing.T) {
	s, err := OpenJSON(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	conformance(t, s)
}

func TestSQLite(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "t.db")+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := OpenSQL(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	conformance(t, s)
	// schema creation must be idempotent across restarts
	if _, err := OpenSQL(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}
