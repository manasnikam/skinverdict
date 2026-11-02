# Devpost submission — SkinVerdict

## Tagline
Is your skincare actually working? A scan-verified verdict — and an agent that reorders, swaps, or refunds based on it.

## Inspiration
The hackathon brief names the moment exactly: *standing in front of a mirror deciding whether a product is working*. Every brand says it works; nobody measures it. Subscriptions churn on vibes, reviews are vibes, and even YouCam's consumer-facing `ui_score` is documented as "adjusted to produce more favorable results". We built the thing that tracks the number underneath.

## What it does
SkinVerdict turns the YouCam AI Skin Analysis API into a post-purchase efficacy loop:

1. **Baseline scan** when you start a product (YouCam JS Camera Kit gates pose, distance and lighting before capture).
2. **Check-ins** at day 7 / 14 / 28 — same concerns, same `raw_score` scale, masks saved for before/after.
3. **Verdict** — *Working / Not yet / Not working* — from a published, deterministic rule set on the product's one claimed concern, with a noise band, a decision horizon, side-effect flags on guard concerns, and adherence (scans after skipped use don't count).
4. **Agent action** — reorder, hold, swap, or the **Proof Guarantee**: a high-confidence *not working* settles the brand's money-back promise as store credit automatically. The scan is the arbitrator.
5. **"People like you"** on the product page — verified outcomes among shoppers with the same skin profile — and a **brand dashboard** showing which SKU fails for which skin profile and what it cost in guarantee credits.

## How we built it
- **YouCam APIs:** `skin-analysis` (SD concerns incl. `skin_type`, `enable_mask_overlay`) and the JS Camera Kit (`skincare` mode) as the capture quality gate. One scan ≈ 2.6 s and 9 units.
- **Go 1.24** stdlib server, SQLite (pure-Go driver, no cgo), single static binary with the **Preact** UI embedded; in-process scheduler for check-ins.
- **Agent:** the Claude Code CLI called headlessly. The verdict is computed first and is final; the model only chooses among the actions the verdict allows and writes the shopper-facing explanation. Off-policy answers or CLI failures fall back to a deterministic rule agent, so check-ins never block.
- Unit-tested verdict engine, store conformance tests against real SQLite, an integration test against a fake YouCam server that mirrors the live response shape, and agent tests against a fake `claude` binary.

## Challenges
- `ui_score` vs `raw_score`: the flattering number moves less than the real one; we display one and judge on the other.
- Longitudinal measurement needs consistent capture — hence Camera Kit as a hard gate, the noise band, and adherence tracking.
- A 4-week product can't be demoed in 48 hours honestly, so we built an ingestion path for dated historical selfies and a labelled "simulate check-in" button rather than faking data.

## Accomplishments
A complete loop — capture → analysis → verdict → agent action → brand analytics — on a stack that deploys as one 11 MB binary, with the scoring rules published in the repo.

## What we learned
Judging a product on *one* claimed concern with guard concerns for side effects is what makes the verdict defensible. Everything else in beauty analytics is a vibe.

## What's next
Claims-substantiation panels for D2C brands (50-person, 4-week cosmetic efficacy cohorts), sampling ROI measurement, routine attribution across multiple products, derm escalation after two failed swaps, and a Shopify app with "Start a verified trial" on the PDP.

## Built with
go, preact, vite, sqlite, youcam-api, youcam-camera-kit, claude-code

---

# Demo video script (target 2:30)

| Time | Screen | Voice |
|---|---|---|
| 0:00–0:15 | Title + a product page with 4.8★ | "Every skincare brand says it works. Nobody measures it. SkinVerdict does." |
| 0:15–0:40 | Start trial → Camera Kit rejecting a bad frame, then capturing | "You start a product, we take a baseline with YouCam's skin analysis. The camera won't capture until lighting and pose are right — that's what makes week-four comparable to week-zero." |
| 0:40–1:05 | Timeline: real scans over 4 weeks, masks before/after, raw vs shown score | "Every check-in re-scans the one concern the product claims to fix. We judge on the raw score — the shown score is the flattering one." |
| 1:05–1:30 | Verdict *Working* → agent reorders, reason text | "Pores up nine raw points in three weeks, outside the noise band: working. The agent reorders — a subscription that only renews when there's evidence." |
| 1:30–1:55 | Second persona: *Not working*, high confidence → Proof Guarantee credit + swap | "Flat at day 28 with high confidence: not working. The brand's money-back promise settles itself as store credit, and the agent swaps to something that won't aggravate the redness we flagged." |
| 1:55–2:10 | Product card: "People like you" line | "Those outcomes feed the product page: twelve oily-skin shoppers, eight working." |
| 2:10–2:25 | Brand dashboard split by profile | "And the brand sees which SKU fails for which skin type — and what it cost in credits." |
| 2:25–2:30 | API names on screen: AI Skin Analysis · JS Camera Kit · 2.6 s · 9 units | "YouCam Skin Analysis, 2.6 seconds a scan. SkinVerdict." |

Recording tips: 1080p, phone or narrow browser window for the shopper screens, desktop for the brand dashboard. No music. Keep cursor movement slow. Numbers on screen must match the voice.

# Screenshots to take (in this order)
1. Product selection with "People like you" line
2. Camera Kit quality overlay mid-capture
3. Timeline with sparkline and masks
4. Verdict card *Working* + reorder action
5. Verdict card *Not working* + Proof Guarantee credit
6. Brand dashboard split by profile
