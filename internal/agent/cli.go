// Package agent decides what to do with a verdict by calling the Claude Code
// CLI in headless mode (`claude -p ... --output-format json`). No API key is
// needed: the CLI uses whatever login it has on the machine. If the CLI is
// missing, errors, or returns something unusable, the deterministic rule
// agent decides instead, so the product never blocks on the model.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/manasnikam/skinverdict/internal/app"
	"github.com/manasnikam/skinverdict/internal/store"
	"github.com/manasnikam/skinverdict/internal/verdict"
)

type CLI struct {
	Bin      string        // "claude"
	Args     []string      // extra args, e.g. --model
	Timeout  time.Duration // per decision
	Fallback app.Agent     // used when the CLI fails
	Log      func(format string, a ...any)
}

func NewCLI() *CLI {
	return &CLI{Bin: "claude", Timeout: 90 * time.Second, Fallback: app.RuleAgent{}, Log: func(string, ...any) {}}
}

// decision is the JSON the model must return.
type decision struct {
	Action    string `json:"action"` // reorder | hold | swap | refund_credit
	SwapTo    string `json:"swap_to"`
	Reason    string `json:"reason"` // 1-3 sentences, shopper-facing
	Rationale string `json:"rationale"`
}

func (c *CLI) Decide(ctx context.Context, in app.DecideInput) (store.Action, error) {
	act, err := c.decide(ctx, in)
	if err != nil {
		c.Log("agent: claude cli failed (%v); using rule agent", err)
		return c.Fallback.Decide(ctx, in)
	}
	return act, nil
}

func (c *CLI) decide(ctx context.Context, in app.DecideInput) (store.Action, error) {
	allowed := allowedActions(in.Verdict)
	prompt := buildPrompt(in, allowed)

	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	args := append([]string{"-p", prompt, "--output-format", "json"}, c.Args...)
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return store.Action{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	text, err := resultText(out.Bytes())
	if err != nil {
		return store.Action{}, err
	}
	var d decision
	// Decoder reads only the first object, so trailing text or a second object doesn't break it.
	if err := json.NewDecoder(strings.NewReader(extractJSON(text))).Decode(&d); err != nil {
		return store.Action{}, fmt.Errorf("model did not return decision JSON: %w (%s)", err, trunc(text, 200))
	}
	if !contains(allowed, d.Action) {
		return store.Action{}, fmt.Errorf("model chose %q, not in allowed %v", d.Action, allowed)
	}
	if d.Reason == "" {
		return store.Action{}, errors.New("empty reason")
	}
	act := store.Action{Kind: d.Action, Reason: d.Reason}
	switch d.Action {
	case "reorder":
		act.OrderRef = "ORD-" + store.NewID()[:6]
	case "swap", "refund_credit":
		if _, ok := in.Catalog.Product(d.SwapTo); !ok || d.SwapTo == in.Product.ID {
			d.SwapTo = firstAlternative(in)
		}
		act.SwapTo = d.SwapTo
		if d.Action == "refund_credit" {
			act.CreditINR = in.Product.PriceINR
			act.OrderRef = "CR-" + store.NewID()[:6]
		}
	}
	c.Log("agent: claude -> %s (%s)", d.Action, trunc(d.Rationale, 120))
	return act, nil
}

// allowedActions keeps the model inside the verdict's policy: it chooses HOW
// to respond, never WHETHER the product worked.
func allowedActions(v verdict.Verdict) []string {
	switch v.Status {
	case verdict.Working:
		if len(v.Worsened) > 0 {
			return []string{"hold", "reorder"}
		}
		return []string{"reorder", "hold"}
	case verdict.NotWorking:
		if v.Confidence == "high" {
			return []string{"refund_credit", "swap"}
		}
		return []string{"swap", "hold"}
	default:
		return []string{"hold"}
	}
}

func buildPrompt(in app.DecideInput, allowed []string) string {
	var b strings.Builder
	b.WriteString("You are the post-purchase agent for SkinVerdict, which measures whether a skincare product is working using YouCam AI skin analysis raw scores (1-100, higher = healthier).\n")
	b.WriteString("The verdict below is final and was computed by a published rule set; you decide only the action and write the shopper-facing reason. Cosmetic language only, never medical claims.\n\n")
	fmt.Fprintf(&b, "Product: %s by %s (₹%d). Claim: %s. Judged on: %s.\n", in.Product.Name, in.Product.Brand, in.Product.PriceINR, in.Product.Claim, in.Catalog.Label(in.Verdict.Target))
	fmt.Fprintf(&b, "Verdict: %s (confidence %s). %s\n", in.Verdict.Status, in.Verdict.Confidence, in.Verdict.Explanation)
	fmt.Fprintf(&b, "Target %s: %.1f -> %.1f (delta %+.1f) over %d days, %d scans, trend %+.1f/week.\n",
		in.Verdict.Target, in.Verdict.Baseline, in.Verdict.Latest, in.Verdict.Delta, in.Verdict.Days, in.Verdict.Scans, in.Verdict.SlopePerWk)
	if len(in.Verdict.Worsened) > 0 {
		fmt.Fprintf(&b, "Guard concerns that worsened: %v\n", in.Verdict.Worsened)
	}
	b.WriteString("Scans (date: concern=raw, adherence):\n")
	for _, s := range in.Scans {
		fmt.Fprintf(&b, "- %s:", s.At.Format("2006-01-02"))
		for k, v := range s.Scores {
			fmt.Fprintf(&b, " %s=%.0f", k, v)
		}
		if s.Adherence != "" {
			fmt.Fprintf(&b, " (%s)", s.Adherence)
		}
		b.WriteString("\n")
	}
	b.WriteString("Swap candidates (id: name — claim, judged on, guards):\n")
	for _, id := range in.Product.Alternatives {
		if p, ok := in.Catalog.Product(id); ok {
			fmt.Fprintf(&b, "- %s: %s — %s, judged on %s, guards %v\n", p.ID, p.Name, p.Claim, p.Target, p.Guards)
		}
	}
	fmt.Fprintf(&b, "\nAllowed actions (pick exactly one): %s.\n", strings.Join(allowed, ", "))
	b.WriteString("Rules: refund_credit means the brand's Proof Guarantee pays store credit because the scan adjudicated the claim; prefer it when allowed. ")
	b.WriteString("When swapping, avoid an alternative that would plausibly aggravate a worsened guard concern. ")
	b.WriteString("Respond with ONLY a JSON object: {\"action\":\"...\",\"swap_to\":\"<id or empty>\",\"reason\":\"1-3 sentences to the shopper, cite the numbers\",\"rationale\":\"one line for the brand\"}\n")
	return b.String()
}

// resultText pulls the assistant text out of `claude --output-format json`.
// Accepts {"result": "..."} (current CLI) or a bare string / raw text.
func resultText(b []byte) (string, error) {
	var env struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if json.Unmarshal(b, &env) == nil && env.Result != "" {
		if env.IsError {
			return "", fmt.Errorf("cli error: %s", trunc(env.Result, 200))
		}
		return env.Result, nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s, nil
	}
	if t := strings.TrimSpace(string(b)); t != "" {
		return t, nil
	}
	return "", errors.New("empty cli output")
}

// extractJSON returns the first {...} block, tolerating ```json fences and prose.
func extractJSON(s string) string {
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}

func firstAlternative(in app.DecideInput) string {
	for _, id := range in.Product.Alternatives {
		if id != in.Product.ID {
			return id
		}
	}
	return ""
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
