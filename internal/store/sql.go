package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SQL implements Store on any database/sql driver speaking SQLite dialect.
// The server uses github.com/ncruces/go-sqlite3 (pure Go, no cgo).
type SQL struct {
	db *sql.DB
}

// OpenSQL opens an existing *sql.DB and creates the schema if missing.
// Drivers are registered by the caller (see cmd/server) so this package
// stays driver-agnostic.
func OpenSQL(ctx context.Context, db *sql.DB) (*SQL, error) {
	s := &SQL{db: db}
	for _, q := range schema {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, fmt.Errorf("schema: %w", err)
		}
	}
	for _, q := range migrations {
		if _, err := db.ExecContext(ctx, q); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrate: %w", err)
		}
	}
	return s, nil
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS personas (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, profile TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS trials (
		id TEXT PRIMARY KEY, persona_id TEXT NOT NULL, product_id TEXT NOT NULL,
		target TEXT NOT NULL, concerns TEXT NOT NULL,
		started_at TEXT NOT NULL, next_check TEXT NOT NULL, status TEXT NOT NULL,
		last_action TEXT)`,
	`CREATE TABLE IF NOT EXISTS scans (
		id TEXT PRIMARY KEY, trial_id TEXT NOT NULL, at TEXT NOT NULL,
		scores TEXT NOT NULL, ui_scores TEXT NOT NULL, overall REAL, skin_age REAL,
		image_path TEXT, mask_paths TEXT, run_ms INTEGER, units REAL, cached INTEGER)`,
	`CREATE INDEX IF NOT EXISTS scans_trial ON scans(trial_id, at)`,
}

// migrations are additive and idempotent: "duplicate column" errors are ignored.
var migrations = []string{
	`ALTER TABLE scans ADD COLUMN adherence TEXT NOT NULL DEFAULT ''`,
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
func js(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------- personas ----------

func (s *SQL) UpsertPersona(p *Persona) error {
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	_, err := s.db.Exec(`INSERT INTO personas(id,name,profile,created_at) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, profile=excluded.profile`,
		p.ID, p.Name, p.Profile, ts(p.CreatedAt))
	return err
}

func (s *SQL) Persona(id string) (*Persona, error) {
	var p Persona
	var c string
	err := s.db.QueryRow(`SELECT id,name,profile,created_at FROM personas WHERE id=?`, id).Scan(&p.ID, &p.Name, &p.Profile, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt = parseTS(c)
	return &p, nil
}

func (s *SQL) Personas() ([]Persona, error) {
	rows, err := s.db.Query(`SELECT id,name,profile,created_at FROM personas ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Persona
	for rows.Next() {
		var p Persona
		var c string
		if err := rows.Scan(&p.ID, &p.Name, &p.Profile, &c); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTS(c)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------- trials ----------

func (s *SQL) CreateTrial(t *Trial) error {
	if t.ID == "" {
		t.ID = NewID()
	}
	if t.StartedAt.IsZero() {
		t.StartedAt = time.Now()
	}
	if t.Status == "" {
		t.Status = "active"
	}
	_, err := s.db.Exec(`INSERT INTO trials(id,persona_id,product_id,target,concerns,started_at,next_check,status,last_action)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		t.ID, t.PersonaID, t.ProductID, t.Target, js(t.Concerns), ts(t.StartedAt), ts(t.NextCheck), t.Status, nullJSON(t.LastAction))
	return err
}

func (s *SQL) UpdateTrial(t *Trial) error {
	res, err := s.db.Exec(`UPDATE trials SET persona_id=?,product_id=?,target=?,concerns=?,started_at=?,next_check=?,status=?,last_action=? WHERE id=?`,
		t.PersonaID, t.ProductID, t.Target, js(t.Concerns), ts(t.StartedAt), ts(t.NextCheck), t.Status, nullJSON(t.LastAction), t.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullJSON(v any) any {
	if v == nil {
		return nil
	}
	if a, ok := v.(*Action); ok && a == nil {
		return nil
	}
	return js(v)
}

const trialCols = `id,persona_id,product_id,target,concerns,started_at,next_check,status,last_action`

func scanTrial(sc interface{ Scan(...any) error }) (*Trial, error) {
	var t Trial
	var concerns, started, next string
	var last sql.NullString
	if err := sc.Scan(&t.ID, &t.PersonaID, &t.ProductID, &t.Target, &concerns, &started, &next, &t.Status, &last); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(concerns), &t.Concerns)
	t.StartedAt, t.NextCheck = parseTS(started), parseTS(next)
	if last.Valid && last.String != "" {
		var a Action
		if json.Unmarshal([]byte(last.String), &a) == nil {
			t.LastAction = &a
		}
	}
	return &t, nil
}

func (s *SQL) Trial(id string) (*Trial, error) {
	t, err := scanTrial(s.db.QueryRow(`SELECT `+trialCols+` FROM trials WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *SQL) Trials() ([]Trial, error) {
	rows, err := s.db.Query(`SELECT ` + trialCols + ` FROM trials ORDER BY started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trial
	for rows.Next() {
		t, err := scanTrial(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ---------- scans ----------

func (s *SQL) AddScan(sc *Scan) error {
	if sc.ID == "" {
		sc.ID = NewID()
	}
	if sc.At.IsZero() {
		sc.At = time.Now()
	}
	cached := 0
	if sc.Cached {
		cached = 1
	}
	_, err := s.db.Exec(`INSERT INTO scans(id,trial_id,at,scores,ui_scores,overall,skin_age,image_path,mask_paths,run_ms,units,cached,adherence)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sc.ID, sc.TrialID, ts(sc.At), js(sc.Scores), js(sc.UIScores), sc.Overall, sc.SkinAge, sc.ImagePath, js(sc.MaskPaths), sc.RunMS, sc.Units, cached, sc.Adherence)
	return err
}

func (s *SQL) Scans(trialID string) ([]Scan, error) {
	rows, err := s.db.Query(`SELECT id,trial_id,at,scores,ui_scores,overall,skin_age,image_path,mask_paths,run_ms,units,cached,adherence
		FROM scans WHERE trial_id=? ORDER BY at`, trialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Scan
	for rows.Next() {
		var sc Scan
		var at, scores, ui, masks string
		var img sql.NullString
		var overall, skinAge, units sql.NullFloat64
		var runMS, cached sql.NullInt64
		if err := rows.Scan(&sc.ID, &sc.TrialID, &at, &scores, &ui, &overall, &skinAge, &img, &masks, &runMS, &units, &cached, &sc.Adherence); err != nil {
			return nil, err
		}
		sc.At = parseTS(at)
		_ = json.Unmarshal([]byte(scores), &sc.Scores)
		_ = json.Unmarshal([]byte(ui), &sc.UIScores)
		_ = json.Unmarshal([]byte(masks), &sc.MaskPaths)
		sc.Overall, sc.SkinAge, sc.Units = overall.Float64, skinAge.Float64, units.Float64
		sc.ImagePath, sc.RunMS, sc.Cached = img.String, runMS.Int64, cached.Int64 == 1
		out = append(out, sc)
	}
	return out, rows.Err()
}
