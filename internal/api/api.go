// Package api exposes the HTTP surface for the Preact front end.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manasnikam/skinverdict/internal/app"
	"github.com/manasnikam/skinverdict/internal/store"
)

type Server struct {
	App *app.App
}

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/catalog", s.catalog)
	mux.HandleFunc("GET /api/units", s.units)

	mux.HandleFunc("GET /api/personas", s.personas)
	mux.HandleFunc("POST /api/personas", s.createPersona)

	mux.HandleFunc("GET /api/trials", s.trials)
	mux.HandleFunc("POST /api/trials", s.createTrial)
	mux.HandleFunc("GET /api/trials/{id}", s.trial)
	mux.HandleFunc("POST /api/trials/{id}/scans", s.scan)
	mux.HandleFunc("POST /api/trials/{id}/checkin", s.checkin)
	mux.HandleFunc("POST /api/admin/checkin-all", s.checkinAll) // demo helper: run check-ins on every active trial with ≥2 scans

	mux.HandleFunc("GET /api/brand/cohorts", s.cohorts)                  // ?by=profile for product × skin-profile rows
	mux.HandleFunc("GET /api/products/{id}/outcomes", s.productOutcomes) // ?profile=oily → "people like you"

	// scan images + masks
	mux.Handle("GET /files/", http.StripPrefix("/files/", http.FileServer(http.Dir(s.App.DataDir))))
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, store.ErrNotFound) {
		code = http.StatusNotFound
	}
	log.Printf("api error: %v", err)
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

// relative URL for a file under DataDir
func (s *Server) fileURL(p string) string {
	if p == "" {
		return ""
	}
	// Rel cleans both sides, so "./data-local" matches stored "data-local/..." paths
	rel, err := filepath.Rel(s.App.DataDir, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return "/files/" + filepath.ToSlash(rel)
}

type scanView struct {
	store.Scan
	ImageURL string            `json:"image_url"`
	MaskURLs map[string]string `json:"mask_urls"`
}

func (s *Server) view(sc store.Scan) scanView {
	v := scanView{Scan: sc, ImageURL: s.fileURL(sc.ImagePath), MaskURLs: map[string]string{}}
	for k, p := range sc.MaskPaths {
		v.MaskURLs[k] = s.fileURL(p)
	}
	return v
}

// ---------- handlers ----------

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "time": time.Now()})
}

func (s *Server) catalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.App.Catalog)
}

func (s *Server) units(w http.ResponseWriter, r *http.Request) {
	n, err := s.App.YouCam.Balance(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]float64{"units": n})
}

func (s *Server) personas(w http.ResponseWriter, _ *http.Request) {
	ps, err := s.App.Store.Personas()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, ps)
}

func (s *Server) createPersona(w http.ResponseWriter, r *http.Request) {
	var p store.Persona
	if err := readJSON(r, &p); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.Store.UpsertPersona(&p); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, p)
}

func (s *Server) trials(w http.ResponseWriter, _ *http.Request) {
	ts, err := s.App.Store.Trials()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, ts)
}

func (s *Server) createTrial(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PersonaID string `json:"persona_id"`
		ProductID string `json:"product_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.App.StartTrial(in.PersonaID, in.ProductID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, t)
}

func (s *Server) trial(w http.ResponseWriter, r *http.Request) {
	t, scans, v, err := s.App.Verdict(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	p, _ := s.App.Catalog.Product(t.ProductID)
	views := make([]scanView, len(scans))
	for i, sc := range scans {
		views[i] = s.view(sc)
	}
	writeJSON(w, 200, map[string]any{"trial": t, "product": p, "scans": views, "verdict": v})
}

// POST /api/trials/{id}/scans — multipart: image=<file>, at=<RFC3339, optional>
// or raw image body with ?at=... . Returns the stored scan.
func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	var img []byte
	var atStr, adherence string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(12 << 20); err != nil {
			writeErr(w, err)
			return
		}
		f, _, err := r.FormFile("image")
		if err != nil {
			writeErr(w, err)
			return
		}
		defer f.Close()
		img, _ = io.ReadAll(f)
		atStr = r.FormValue("at")
		adherence = r.FormValue("adherence")
	} else {
		img, _ = io.ReadAll(io.LimitReader(r.Body, 12<<20))
		atStr = r.URL.Query().Get("at")
		adherence = r.URL.Query().Get("adherence")
	}
	switch adherence {
	case "", "directed", "mostly", "skipped":
	default:
		writeJSON(w, 400, map[string]string{"error": "adherence must be directed|mostly|skipped"})
		return
	}
	if len(img) == 0 {
		writeJSON(w, 400, map[string]string{"error": "no image"})
		return
	}
	var at time.Time
	if atStr != "" {
		t, err := time.Parse(time.RFC3339, atStr)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad at: " + err.Error()})
			return
		}
		at = t
	}
	sc, err := s.App.Scan(r.Context(), r.PathValue("id"), img, at, adherence)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, s.view(*sc))
}

func (s *Server) checkin(w http.ResponseWriter, r *http.Request) {
	t, v, err := s.App.CheckIn(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"trial": t, "verdict": v, "action": t.LastAction})
}

type cohortRow struct {
	ProductID string         `json:"product_id"`
	Name      string         `json:"name"`
	Target    string         `json:"target"`
	Profile   string         `json:"profile,omitempty"` // set when grouped by skin profile
	Trials    int            `json:"trials"`
	Judged    int            `json:"judged"` // trials with ≥2 scans (verdict is meaningful)
	ByStatus  map[string]int `json:"by_status"`
	MeanDelta float64        `json:"mean_delta"`
	Worsened  map[string]int `json:"worsened"`
	Credits   int            `json:"credits_issued"`
	CreditINR int            `json:"credits_inr"`
}

// aggregate builds product (× profile) rows from every trial's current verdict.
func (s *Server) aggregate(byProfile bool) ([]cohortRow, error) {
	ts, err := s.App.Store.Trials()
	if err != nil {
		return nil, err
	}
	profiles := map[string]string{}
	if ps, err := s.App.Store.Personas(); err == nil {
		for _, p := range ps {
			profiles[p.ID] = p.Profile
		}
	}
	rows := map[string]*cohortRow{}
	for _, t := range ts {
		_, scans, v, err := s.App.Verdict(t.ID)
		if err != nil {
			continue
		}
		key := t.ProductID
		prof := ""
		if byProfile {
			prof = profiles[t.PersonaID]
			if prof == "" {
				prof = "unknown"
			}
			key += "|" + prof
		}
		rw := rows[key]
		if rw == nil {
			p, _ := s.App.Catalog.Product(t.ProductID)
			rw = &cohortRow{ProductID: t.ProductID, Name: p.Name, Target: p.Target, Profile: prof, ByStatus: map[string]int{}, Worsened: map[string]int{}}
			rows[key] = rw
		}
		rw.Trials++
		if len(scans) >= 2 {
			rw.Judged++
			rw.ByStatus[string(v.Status)]++
			rw.MeanDelta += v.Delta
		}
		for k := range v.Worsened {
			rw.Worsened[k]++
		}
		if t.LastAction != nil && t.LastAction.Kind == "refund_credit" {
			rw.Credits++
			rw.CreditINR += t.LastAction.CreditINR
		}
	}
	out := make([]cohortRow, 0, len(rows))
	for _, rw := range rows {
		if rw.Judged > 0 {
			rw.MeanDelta = float64(int(rw.MeanDelta/float64(rw.Judged)*10)) / 10
		}
		out = append(out, *rw)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProductID != out[j].ProductID {
			return out[i].ProductID < out[j].ProductID
		}
		return out[i].Profile < out[j].Profile
	})
	return out, nil
}

func (s *Server) checkinAll(w http.ResponseWriter, r *http.Request) {
	ts, err := s.App.Store.Trials()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []map[string]any{}
	for _, t := range ts {
		if t.Status != "active" {
			continue
		}
		if scans, _ := s.App.Store.Scans(t.ID); len(scans) < 2 {
			continue
		}
		t2, v, err := s.App.CheckIn(r.Context(), t.ID)
		if err != nil {
			out = append(out, map[string]any{"trial": t.ID, "error": err.Error()})
			continue
		}
		out = append(out, map[string]any{"trial": t.ID, "status": v.Status, "action": t2.LastAction.Kind})
	}
	writeJSON(w, 200, out)
}

// GET /api/brand/cohorts[?by=profile] — brand dashboard rows.
func (s *Server) cohorts(w http.ResponseWriter, r *http.Request) {
	out, err := s.aggregate(r.URL.Query().Get("by") == "profile")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// GET /api/products/{id}/outcomes?profile=oily — "people like you" for the PDP.
// Returns the matching-profile row when it exists, else the all-profiles row.
func (s *Server) productOutcomes(w http.ResponseWriter, r *http.Request) {
	id, prof := r.PathValue("id"), r.URL.Query().Get("profile")
	all, err := s.aggregate(false)
	if err != nil {
		writeErr(w, err)
		return
	}
	resp := map[string]any{"product_id": id, "profile": prof}
	for _, rw := range all {
		if rw.ProductID == id {
			resp["all"] = rw
		}
	}
	if prof != "" {
		byP, _ := s.aggregate(true)
		for _, rw := range byP {
			if rw.ProductID == id && rw.Profile == prof {
				resp["matching"] = rw
			}
		}
	}
	writeJSON(w, 200, resp)
}
