// Package verdict decides whether a skincare product is working, from a series
// of YouCam skin-analysis scans. It is deterministic and dependency-free so the
// rules can be published and unit-tested; the agent layer decides what to DO
// with a verdict (reorder / hold / swap), never what the verdict IS.
//
// All scores are YouCam raw_score values (1-100, higher = healthier).
package verdict

import (
	"fmt"
	"math"
	"sort"
	"time"
)

type Scan struct {
	At     time.Time          `json:"at"`
	Scores map[string]float64 `json:"scores"` // concern -> raw_score
}

type Status string

const (
	Working    Status = "working"     // target concern improved beyond the noise band
	NotYet     Status = "not_yet"     // too early or still inside the noise band
	NotWorking Status = "not_working" // worsened, or no movement by the decision horizon
)

type Options struct {
	NoiseBand    float64 // |Δ| below this is treated as measurement noise (raw points)
	WorsenBand   float64 // another concern dropping by more than this is flagged
	MinDays      int     // before this, only a very large gain counts as Working
	HorizonDays  int     // by this point, "no movement" becomes NotWorking
	EarlyWinMult float64 // gain needed before MinDays, as a multiple of NoiseBand
}

func DefaultOptions() Options {
	return Options{NoiseBand: 4, WorsenBand: 6, MinDays: 14, HorizonDays: 28, EarlyWinMult: 2}
}

type Verdict struct {
	Status      Status             `json:"status"`
	Target      string             `json:"target"`
	Baseline    float64            `json:"baseline"`
	Latest      float64            `json:"latest"`
	Delta       float64            `json:"delta"`        // latest - baseline, raw points (+ = better)
	SlopePerWk  float64            `json:"slope_per_wk"` // least-squares trend across all scans
	Days        int                `json:"days"`
	Scans       int                `json:"scans"`
	Worsened    map[string]float64 `json:"worsened,omitempty"` // other concerns that dropped beyond WorsenBand
	Confidence  string             `json:"confidence"`         // low | medium | high
	Explanation string             `json:"explanation"`
}

// Evaluate computes the verdict for `target` over the given scans (any order).
func Evaluate(target string, scans []Scan, opt Options) (Verdict, error) {
	if len(scans) == 0 {
		return Verdict{}, fmt.Errorf("verdict: no scans")
	}
	ss := append([]Scan(nil), scans...)
	sort.Slice(ss, func(i, j int) bool { return ss[i].At.Before(ss[j].At) })
	base, last := ss[0], ss[len(ss)-1]
	b, okB := base.Scores[target]
	l, okL := last.Scores[target]
	if !okB || !okL {
		return Verdict{}, fmt.Errorf("verdict: target %q missing from baseline or latest scan", target)
	}
	days := int(math.Round(last.At.Sub(base.At).Hours() / 24))
	v := Verdict{Target: target, Baseline: b, Latest: l, Delta: l - b, Days: days, Scans: len(ss)}

	// trend across all scans (points per week)
	v.SlopePerWk = slopePerWeek(ss, target)

	// side effects: other concerns that got worse
	for k, bv := range base.Scores {
		if k == target {
			continue
		}
		if lv, ok := last.Scores[k]; ok && lv-bv < -opt.WorsenBand {
			if v.Worsened == nil {
				v.Worsened = map[string]float64{}
			}
			v.Worsened[k] = round1(lv - bv)
		}
	}

	switch {
	case len(ss) == 1:
		v.Status = NotYet
	case v.Delta >= opt.NoiseBand*opt.EarlyWinMult:
		v.Status = Working // big enough to count even before MinDays
	case days < opt.MinDays:
		v.Status = NotYet
	case v.Delta >= opt.NoiseBand:
		v.Status = Working
	case v.Delta <= -opt.NoiseBand:
		v.Status = NotWorking
	case days >= opt.HorizonDays:
		v.Status = NotWorking // flat at the horizon = not doing anything
	default:
		v.Status = NotYet
	}

	// confidence: more scans + longer window + agreement between delta and trend
	agree := (v.Delta >= 0) == (v.SlopePerWk >= 0)
	switch {
	case len(ss) >= 4 && days >= opt.HorizonDays && agree:
		v.Confidence = "high"
	case len(ss) >= 3 && days >= opt.MinDays:
		v.Confidence = "medium"
	default:
		v.Confidence = "low"
	}
	v.Delta = round1(v.Delta)
	v.SlopePerWk = round1(v.SlopePerWk)
	v.Explanation = explain(v, opt)
	return v, nil
}

func explain(v Verdict, opt Options) string {
	dir := "improved"
	if v.Delta < 0 {
		dir = "worsened"
	}
	s := fmt.Sprintf("%s %s by %.1f raw points over %d days across %d scans (noise band ±%.0f).",
		v.Target, dir, math.Abs(v.Delta), v.Days, v.Scans, opt.NoiseBand)
	switch v.Status {
	case Working:
		s += " That is outside the noise band, so the product is doing something."
	case NotYet:
		if v.Days < opt.MinDays {
			s += fmt.Sprintf(" Fewer than %d days in; keep going before judging.", opt.MinDays)
		} else {
			s += " Movement is still inside the noise band; keep going until the next check-in."
		}
	case NotWorking:
		if v.Delta <= -opt.NoiseBand {
			s += " The target concern got worse; stop and swap."
		} else {
			s += fmt.Sprintf(" No real movement by day %d; this product is not working for you.", opt.HorizonDays)
		}
	}
	if len(v.Worsened) > 0 {
		s += " Side effects flagged:"
		for k, d := range v.Worsened {
			s += fmt.Sprintf(" %s %.1f;", k, d)
		}
	}
	return s
}

func slopePerWeek(ss []Scan, target string) float64 {
	n := 0.0
	var sx, sy, sxx, sxy float64
	t0 := ss[0].At
	for _, s := range ss {
		y, ok := s.Scores[target]
		if !ok {
			continue
		}
		x := s.At.Sub(t0).Hours() / (24 * 7)
		n++
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
	}
	den := n*sxx - sx*sx
	if n < 2 || den == 0 {
		return 0
	}
	return (n*sxy - sx*sy) / den
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
