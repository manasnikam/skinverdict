// Package store persists personas, trials and scans. The JSON implementation
// has zero dependencies and is sufficient for hackathon-scale data; it is
// behind an interface so it can be swapped for SQLite without touching callers.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type Persona struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Profile   string    `json:"profile"` // e.g. "oily", "dry", "combination" — from skin_type when known
	CreatedAt time.Time `json:"created_at"`
}

type Trial struct {
	ID         string    `json:"id"`
	PersonaID  string    `json:"persona_id"`
	ProductID  string    `json:"product_id"`
	Target     string    `json:"target"`   // concern this trial is judged on
	Concerns   []string  `json:"concerns"` // all concerns scanned each time (target + guards)
	StartedAt  time.Time `json:"started_at"`
	NextCheck  time.Time `json:"next_check"`
	Status     string    `json:"status"` // active | reordered | held | swapped | credited | closed
	LastAction *Action   `json:"last_action,omitempty"`
}

type Scan struct {
	ID        string             `json:"id"`
	TrialID   string             `json:"trial_id"`
	At        time.Time          `json:"at"` // photo date for ingested history, now() for live scans
	Scores    map[string]float64 `json:"scores"`
	UIScores  map[string]float64 `json:"ui_scores"`
	Overall   float64            `json:"overall"`
	SkinAge   float64            `json:"skin_age"`
	ImagePath string             `json:"image_path"`
	MaskPaths map[string]string  `json:"mask_paths"` // concern -> local file
	RunMS     int64              `json:"run_ms"`
	Units     float64            `json:"units"`
	Cached    bool               `json:"cached"`
	Adherence string             `json:"adherence"` // since last scan: directed | mostly | skipped ("" = unknown)
}

type Action struct {
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"` // reorder | hold | swap | refund_credit
	Reason    string    `json:"reason"`
	Verdict   any       `json:"verdict"`
	SwapTo    string    `json:"swap_to,omitempty"`
	OrderRef  string    `json:"order_ref,omitempty"`
	CreditINR int       `json:"credit_inr,omitempty"` // Proof Guarantee: store credit issued on a verified not_working
}

type Store interface {
	UpsertPersona(p *Persona) error
	Persona(id string) (*Persona, error)
	Personas() ([]Persona, error)

	CreateTrial(t *Trial) error
	UpdateTrial(t *Trial) error
	Trial(id string) (*Trial, error)
	Trials() ([]Trial, error)

	AddScan(s *Scan) error
	Scans(trialID string) ([]Scan, error)
}

// ---------- JSON file implementation ----------

type data struct {
	Personas map[string]Persona `json:"personas"`
	Trials   map[string]Trial   `json:"trials"`
	Scans    map[string]Scan    `json:"scans"`
}

type JSON struct {
	path string
	mu   sync.RWMutex
	d    data
}

func OpenJSON(path string) (*JSON, error) {
	j := &JSON{path: path, d: data{map[string]Persona{}, map[string]Trial{}, map[string]Scan{}}}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &j.d); err != nil {
			return nil, err
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	return j, nil
}

func (j *JSON) save() error {
	b, err := json.MarshalIndent(j.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, j.path)
}

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (j *JSON) UpsertPersona(p *Persona) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	j.d.Personas[p.ID] = *p
	return j.save()
}

func (j *JSON) Persona(id string) (*Persona, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	p, ok := j.d.Personas[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &p, nil
}

func (j *JSON) Personas() ([]Persona, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make([]Persona, 0, len(j.d.Personas))
	for _, p := range j.d.Personas {
		out = append(out, p)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.Before(out[b].CreatedAt) })
	return out, nil
}

func (j *JSON) CreateTrial(t *Trial) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if t.ID == "" {
		t.ID = NewID()
	}
	if t.StartedAt.IsZero() {
		t.StartedAt = time.Now()
	}
	if t.Status == "" {
		t.Status = "active"
	}
	j.d.Trials[t.ID] = *t
	return j.save()
}

func (j *JSON) UpdateTrial(t *Trial) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, ok := j.d.Trials[t.ID]; !ok {
		return ErrNotFound
	}
	j.d.Trials[t.ID] = *t
	return j.save()
}

func (j *JSON) Trial(id string) (*Trial, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	t, ok := j.d.Trials[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &t, nil
}

func (j *JSON) Trials() ([]Trial, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make([]Trial, 0, len(j.d.Trials))
	for _, t := range j.d.Trials {
		out = append(out, t)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt.Before(out[b].StartedAt) })
	return out, nil
}

func (j *JSON) AddScan(s *Scan) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if s.ID == "" {
		s.ID = NewID()
	}
	if s.At.IsZero() {
		s.At = time.Now()
	}
	j.d.Scans[s.ID] = *s
	return j.save()
}

func (j *JSON) Scans(trialID string) ([]Scan, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	var out []Scan
	for _, s := range j.d.Scans {
		if s.TrialID == trialID {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].At.Before(out[b].At) })
	return out, nil
}
