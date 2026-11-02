// Package app wires catalog, store, YouCam and the verdict engine into the
// two operations the product is built on: Scan and CheckIn.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/manasnikam/skinverdict/internal/catalog"
	"github.com/manasnikam/skinverdict/internal/store"
	"github.com/manasnikam/skinverdict/internal/verdict"
	"github.com/manasnikam/skinverdict/internal/youcam"
)

// Agent decides what to do with a verdict. The rule agent is deterministic;
// a Claude-backed agent can replace it without touching the rest.
type Agent interface {
	Decide(ctx context.Context, in DecideInput) (store.Action, error)
}

type DecideInput struct {
	Trial   store.Trial
	Product catalog.Product
	Verdict verdict.Verdict
	Scans   []store.Scan
	Catalog *catalog.Catalog
}

type App struct {
	Store    store.Store
	Catalog  *catalog.Catalog
	YouCam   *youcam.Client
	Agent    Agent
	DataDir  string // scan images + masks live under DataDir/scans/<scanID>/
	Options  verdict.Options
	CheckGap time.Duration // time between check-ins (7 days in prod, minutes in demo)
}

// StartTrial creates a trial for persona+product. The first scan is the baseline.
func (a *App) StartTrial(personaID, productID string) (*store.Trial, error) {
	p, ok := a.Catalog.Product(productID)
	if !ok {
		return nil, fmt.Errorf("unknown product %q", productID)
	}
	if _, err := a.Store.Persona(personaID); err != nil {
		return nil, fmt.Errorf("persona: %w", err)
	}
	concerns := a.Catalog.Concerns(p)
	if len(concerns) < 4 { // free profile signal while staying in the 1-4 concern tier (9 units)
		concerns = append(concerns, "skin_type")
	}
	t := &store.Trial{
		PersonaID: personaID, ProductID: productID,
		Target: p.Target, Concerns: concerns,
		StartedAt: time.Now(), NextCheck: time.Now().Add(a.CheckGap), Status: "active",
	}
	if err := a.Store.CreateTrial(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Scan runs skin analysis on a selfie for the trial's concerns and stores it.
// `at` overrides the scan time (used when ingesting dated historical photos).
// `adherence` is the shopper's answer to "used as directed since last scan?":
// directed | mostly | skipped | "" (unknown).
func (a *App) Scan(ctx context.Context, trialID string, selfie []byte, at time.Time, adherence string) (*store.Scan, error) {
	t, err := a.Store.Trial(trialID)
	if err != nil {
		return nil, err
	}
	sa, err := a.YouCam.SkinAnalyze(ctx, selfie, t.Concerns)
	if err != nil {
		return nil, err
	}
	s := &store.Scan{
		ID: store.NewID(), TrialID: t.ID, At: at,
		Scores: map[string]float64{}, UIScores: map[string]float64{}, MaskPaths: map[string]string{},
		Overall: sa.Overall, SkinAge: sa.SkinAge,
		RunMS: sa.Task.RunMS, Units: sa.Task.UnitsCost, Cached: sa.Task.Cached,
		Adherence: adherence,
	}
	if s.At.IsZero() {
		s.At = time.Now()
	}
	dir := filepath.Join(a.DataDir, "scans", s.ID)
	_ = os.MkdirAll(dir, 0o755)
	s.ImagePath = filepath.Join(dir, "selfie.jpg")
	_ = os.WriteFile(s.ImagePath, selfie, 0o644)
	for concern, cs := range sa.Concerns {
		s.Scores[concern] = cs.RawScore
		s.UIScores[concern] = cs.UIScore
		if len(cs.MaskURLs) > 0 {
			// presigned URLs expire in ~2h: persist the mask now
			if b, err := a.YouCam.Download(ctx, cs.MaskURLs[0]); err == nil {
				p := filepath.Join(dir, concern+".jpg")
				if os.WriteFile(p, b, 0o644) == nil {
					s.MaskPaths[concern] = p
				}
			}
		}
	}
	if err := a.Store.AddScan(s); err != nil {
		return nil, err
	}
	// skin profile: from the engine's skin_type when available, else a documented
	// heuristic on oiliness/moisture raw scores (lower = worse).
	if profile := profileOf(sa.SkinType(), s.Scores); profile != "" {
		if p, err := a.Store.Persona(t.PersonaID); err == nil && p.Profile == "" {
			p.Profile = profile
			_ = a.Store.UpsertPersona(p)
		}
	}
	return s, nil
}

func profileOf(skinType string, scores map[string]float64) string {
	if skinType != "" {
		return skinType
	}
	oil, hasOil := scores["oiliness"]
	moist, hasMoist := scores["moisture"]
	switch {
	case hasOil && hasMoist && oil < 55 && moist < 55:
		return "combination"
	case hasOil && oil < 55:
		return "oily"
	case hasMoist && moist < 55:
		return "dry"
	case hasOil || hasMoist:
		return "normal"
	}
	return ""
}

// Verdict computes the current verdict for a trial from its stored scans.
func (a *App) Verdict(trialID string) (*store.Trial, []store.Scan, verdict.Verdict, error) {
	t, err := a.Store.Trial(trialID)
	if err != nil {
		return nil, nil, verdict.Verdict{}, err
	}
	scans, err := a.Store.Scans(t.ID)
	if err != nil {
		return nil, nil, verdict.Verdict{}, err
	}
	if len(scans) == 0 {
		return t, scans, verdict.Verdict{Status: verdict.NotYet, Target: t.Target, Explanation: "No scans yet."}, nil
	}
	// Adherence: a scan taken after a skipped stretch says nothing about the
	// product, so it is excluded from the judgement (the baseline always counts).
	var vs []verdict.Scan
	skipped := 0
	for i, s := range scans {
		if i > 0 && s.Adherence == "skipped" {
			skipped++
			continue
		}
		vs = append(vs, verdict.Scan{At: s.At, Scores: s.Scores})
	}
	v, err := verdict.Evaluate(t.Target, vs, a.Options)
	if err != nil {
		return t, scans, v, err
	}
	if skipped > 0 {
		v.Explanation += fmt.Sprintf(" %d scan(s) after skipped use were excluded.", skipped)
	}
	if last := scans[len(scans)-1]; last.Adherence == "skipped" && v.Status == verdict.Working {
		v.Confidence = "low"
	}
	return t, scans, v, nil
}

// CheckIn is what the scheduler (or the demo fast-forward button) runs:
// compute the verdict, let the agent act, record the action on the trial.
func (a *App) CheckIn(ctx context.Context, trialID string) (*store.Trial, verdict.Verdict, error) {
	t, scans, v, err := a.Verdict(trialID)
	if err != nil {
		return nil, v, err
	}
	if t.Status == "credited" || t.Status == "swapped" {
		return t, v, nil // closed: re-running would issue a second credit or swap
	}
	p, _ := a.Catalog.Product(t.ProductID)
	act, err := a.Agent.Decide(ctx, DecideInput{Trial: *t, Product: p, Verdict: v, Scans: scans, Catalog: a.Catalog})
	if err != nil {
		return nil, v, err
	}
	act.At = time.Now()
	act.Verdict = v
	t.LastAction = &act
	switch act.Kind {
	case "reorder":
		t.Status = "reordered"
	case "swap":
		t.Status = "swapped"
	case "refund_credit":
		t.Status = "credited"
	default:
		t.Status = "active"
	}
	t.NextCheck = time.Now().Add(a.CheckGap)
	if err := a.Store.UpdateTrial(t); err != nil {
		return nil, v, err
	}
	return t, v, nil
}

// ---------- deterministic rule agent (Claude agent replaces this on Sunday) ----------

type RuleAgent struct{}

func (RuleAgent) Decide(_ context.Context, in DecideInput) (store.Action, error) {
	switch in.Verdict.Status {
	case verdict.Working:
		if len(in.Verdict.Worsened) > 0 {
			return store.Action{Kind: "hold", Reason: "Target improved but a guard concern worsened; keep using, do not reorder yet, and re-check at the next scan."}, nil
		}
		return store.Action{Kind: "reorder", OrderRef: "ORD-" + store.NewID()[:6],
			Reason: fmt.Sprintf("%s improved %.1f raw points over %d days. Replenishment ordered.", in.Catalog.Label(in.Verdict.Target), in.Verdict.Delta, in.Verdict.Days)}, nil
	case verdict.NotWorking:
		swap := ""
		for _, alt := range in.Product.Alternatives {
			if alt != in.Product.ID {
				swap = alt
				break
			}
		}
		if in.Verdict.Confidence == "high" {
			// Proof Guarantee: the scan adjudicated the claim, so the brand's
			// money-back promise is settled automatically as store credit.
			return store.Action{Kind: "refund_credit", SwapTo: swap, CreditINR: in.Product.PriceINR, OrderRef: "CR-" + store.NewID()[:6],
				Reason: fmt.Sprintf("%s did not improve over %d days with high confidence (Δ %.1f across %d scans). Proof Guarantee triggered: ₹%d store credit issued, and %s suggested as the swap.",
					in.Catalog.Label(in.Verdict.Target), in.Verdict.Days, in.Verdict.Delta, in.Verdict.Scans, in.Product.PriceINR, altName(in, swap))}, nil
		}
		return store.Action{Kind: "swap", SwapTo: swap,
			Reason: fmt.Sprintf("%s did not improve (Δ %.1f over %d days). Stopping %s and switching to %s.", in.Catalog.Label(in.Verdict.Target), in.Verdict.Delta, in.Verdict.Days, in.Product.Name, altName(in, swap))}, nil
	default:
		return store.Action{Kind: "hold", Reason: "Too early to judge; keep using and scan again at the next check-in."}, nil
	}
}

func altName(in DecideInput, id string) string {
	if p, ok := in.Catalog.Product(id); ok {
		return p.Name
	}
	return "an alternative"
}
