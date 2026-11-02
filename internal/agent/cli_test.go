package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manasnikam/skinverdict/internal/app"
	"github.com/manasnikam/skinverdict/internal/catalog"
	"github.com/manasnikam/skinverdict/internal/store"
	"github.com/manasnikam/skinverdict/internal/verdict"
)

// fakeClaude writes a shell script that mimics `claude -p ... --output-format json`,
// printing {"result": <text>} where text is what the model would have said.
func fakeClaude(t *testing.T, modelText string) string {
	t.Helper()
	dir := t.TempDir()
	env, _ := json.Marshal(map[string]any{"result": modelText, "is_error": false})
	if err := os.WriteFile(filepath.Join(dir, "out.json"), env, 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\ncat \""+filepath.Join(dir, "out.json")+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func input(t *testing.T, v verdict.Verdict) app.DecideInput {
	t.Helper()
	cat, err := catalog.Load(filepath.Join("..", "..", "data", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := cat.Product("nia10")
	return app.DecideInput{
		Trial: store.Trial{ID: "t1", ProductID: "nia10", Target: "pore"}, Product: p, Verdict: v, Catalog: cat,
		Scans: []store.Scan{{At: time.Now().AddDate(0, 0, -28), Scores: map[string]float64{"pore": 30}}, {At: time.Now(), Scores: map[string]float64{"pore": 28}, Adherence: "directed"}},
	}
}

func TestRefundCreditFromModel(t *testing.T) {
	c := NewCLI()
	c.Bin = fakeClaude(t, "Here you go:\n```json\n{\"action\":\"refund_credit\",\"swap_to\":\"azel10\",\"reason\":\"Pores went 30 to 28 over 28 days, so this did not work for you.\",\"rationale\":\"flat\"}\n```")
	act, err := c.Decide(context.Background(), input(t, verdict.Verdict{Status: verdict.NotWorking, Confidence: "high", Target: "pore", Baseline: 30, Latest: 28, Delta: -2, Days: 28, Scans: 4}))
	if err != nil {
		t.Fatal(err)
	}
	if act.Kind != "refund_credit" || act.SwapTo != "azel10" || act.CreditINR != 699 || act.OrderRef == "" {
		t.Fatalf("%+v", act)
	}
}

func TestTrailingObjectIgnored(t *testing.T) {
	c := NewCLI()
	c.Bin = fakeClaude(t, `{"action":"refund_credit","reason":"model reason"}, {"note":"extra"}`)
	act, err := c.Decide(context.Background(), input(t, verdict.Verdict{Status: verdict.NotWorking, Confidence: "high", Target: "pore", Delta: -2, Days: 28, Scans: 4}))
	if err != nil || act.Kind != "refund_credit" || act.Reason != "model reason" {
		t.Fatalf("%v %+v", err, act)
	}
}

func TestDisallowedActionFallsBack(t *testing.T) {
	c := NewCLI()
	// model tries to reorder a not-working product: rejected, rule agent decides
	c.Bin = fakeClaude(t, `{"action":"reorder","reason":"x"}`)
	act, err := c.Decide(context.Background(), input(t, verdict.Verdict{Status: verdict.NotWorking, Confidence: "medium", Target: "pore", Delta: -5, Days: 21, Scans: 3}))
	if err != nil || act.Kind != "swap" {
		t.Fatalf("%v %+v", err, act)
	}
}

func TestMissingBinaryFallsBack(t *testing.T) {
	c := NewCLI()
	c.Bin = "/nonexistent/claude"
	act, err := c.Decide(context.Background(), input(t, verdict.Verdict{Status: verdict.Working, Confidence: "medium", Target: "pore", Delta: 6, Days: 21, Scans: 3}))
	if err != nil || act.Kind != "reorder" {
		t.Fatalf("%v %+v", err, act)
	}
}

func TestBadSwapIDReplaced(t *testing.T) {
	c := NewCLI()
	c.Bin = fakeClaude(t, `{"action":"swap","swap_to":"made-up","reason":"ok"}`)
	act, err := c.Decide(context.Background(), input(t, verdict.Verdict{Status: verdict.NotWorking, Confidence: "medium", Target: "pore", Delta: -5, Days: 21, Scans: 3}))
	if err != nil || act.Kind != "swap" || act.SwapTo != "azel10" {
		t.Fatalf("%v %+v", err, act)
	}
}

func TestPromptMentionsAllowedActions(t *testing.T) {
	in := input(t, verdict.Verdict{Status: verdict.Working, Worsened: map[string]float64{"redness": -8}})
	p := buildPrompt(in, allowedActions(in.Verdict))
	for _, want := range []string{"hold, reorder", "redness", "Niacinamide", "azel10"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}
