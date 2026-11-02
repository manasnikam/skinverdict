# Devpost submission — SkinVerdict

## Project name
SkinVerdict

## Elevator pitch (192 characters)
Is your skincare actually working? SkinVerdict re-scans your skin with YouCam AI, gives a fair verdict on the one thing a product claims to fix, and reorders, swaps or refunds as store credit.

## Description
SkinVerdict answers the question every skincare buyer asks in front of the mirror: is this actually working? It turns the YouCam AI Skin Analysis API into a trial that ends in a verdict, and the verdict decides what happens to the shopper's money.

**How it works for the shopper**
- **Start a trial.** Pick a product and take a baseline selfie. The YouCam JS Camera Kit won't capture until face position, angle and lighting are right, so a week-4 scan compares fairly with day 0.
- **Check in weekly.** Each scan is scored by YouCam on the one concern the product claims to fix (pores for a niacinamide serum, breakouts for salicylic acid), plus "guard" concerns that would show a side effect. Before/after masks show where the change happened.
- **Get a verdict.** Working, Not yet, or Not working, from published rules: a ±4-point noise band, a 28-day decision horizon, side-effect flags, and adherence (scans after skipped use don't count). With 3+ scans it counts only the change that both the first-vs-latest scores and a robust trend agree on, so one bad photo can't decide the result. It judges YouCam's raw score, not the friendlier score shoppers usually see, and shows both.
- **The agent acts on it.** Working: the next bottle is reordered. Too early: it holds. Not working: the brand's money-back promise (the Proof Guarantee) is settled as store credit automatically, and the agent suggests an alternative product to try next. The scan is the arbitrator, not a support ticket.
- **People like you.** Each product card shows verified outcomes for shoppers with the same skin type, e.g. "1 of 2 oily-skin shoppers: working".

**Value for the shopper:** no more paying for months of a product on faith. You find out in four weeks, with evidence, and you don't lose money when it doesn't work.

**Value for the retailer and brand**
- **Renewals that stick:** reorders happen when there's evidence the product works, instead of subscriptions that churn when customers lose faith.
- **A guarantee that's cheap to run:** refunds are decided by a measurement, not by arguing with customers, and are paid as store credit that stays in the shop.
- **A brand dashboard** with success rate, guarantee credits paid, reorder revenue and outcomes by skin type. It shows, for example, that a serum works for oily and combination skin but fails 2 of 2 times for dry skin, so the brand can stop recommending it to dry skin before more refunds pile up.
- **Proof for marketing claims:** "scan-verified" results replace star ratings as the trust signal on the product page.

## Inspiration
The hackathon brief names the moment exactly: *standing in front of a mirror deciding whether a product is working*. Every brand says it works; nobody measures it. Subscriptions churn on vibes, reviews are vibes, and even YouCam's consumer-facing `ui_score` is documented as "adjusted to produce more favorable results". We built the thing that tracks the number underneath.

## What it does
SkinVerdict turns the YouCam AI Skin Analysis API into a post-purchase efficacy loop:

1. **Baseline scan** when you start a product (YouCam JS Camera Kit gates pose, distance and lighting before capture).
2. **Check-ins** at day 7 / 14 / 28 — same concerns, same `raw_score` scale, masks saved for before/after.
3. **Verdict** — *Working / Not yet / Not working* — from a published, deterministic rule set on the product's one claimed concern, with a ±4 noise band, a 28-day decision horizon, side-effect flags on guard concerns, and adherence (scans after skipped use don't count). With 3+ scans it counts only the change that both first-vs-latest and a robust Theil–Sen trend agree on, so one bad scan can't trigger (or block) a refund.
4. **Agent action** — reorder, hold, swap, or the **Proof Guarantee**: a high-confidence *not working* settles the brand's money-back promise as store credit automatically. The scan is the arbitrator.
5. **"People like you"** on the product page — verified outcomes among shoppers with the same skin profile — and a **brand dashboard** showing which SKU fails for which skin profile and what it cost in guarantee credits.

## How we built it
- **YouCam APIs:** `skin-analysis` (SD concerns incl. `skin_type`, `enable_mask_overlay`) and the JS Camera Kit (`skincare` mode) as the capture quality gate. One scan takes about 3–6 s and 9 units.
- **Go 1.24** stdlib server, SQLite (pure-Go driver, no cgo), single static binary with the **Preact** UI embedded; in-process scheduler for check-ins.
- **Agent:** the Claude Code CLI called headlessly. The verdict is computed first and is final; the model only chooses among the actions the verdict allows and writes the shopper-facing explanation. Off-policy answers or CLI failures fall back to a deterministic rule agent, so check-ins never block.
- Unit-tested verdict engine, store conformance tests against real SQLite, an integration test against a fake YouCam server that mirrors the live response shape, and agent tests against a fake `claude` binary.

## Challenges
- `ui_score` vs `raw_score`: the flattering number moves less than the real one; we display one and judge on the other.
- Longitudinal measurement needs consistent capture — hence Camera Kit as a hard gate, the noise band, and adherence tracking.
- Scan noise is real: five photos of one unchanged face, varying only crop and exposure, scored 81–97 on pores. That's why the verdict judges a robust trend instead of two endpoints.
- A 4-week product can't be demoed in 48 hours honestly, so we built an ingestion path for dated historical selfies and a labelled "simulate check-in" button rather than faking data.

## Accomplishments
A complete loop — capture → analysis → verdict → agent action → brand analytics — on a stack that deploys as one 11 MB binary, with the scoring rules published in the repo.

## What we learned
Judging a product on *one* claimed concern with guard concerns for side effects is what makes the verdict defensible. Everything else in beauty analytics is a vibe.

## What's next
Claims-substantiation panels for D2C brands (50-person, 4-week cosmetic efficacy cohorts), sampling ROI measurement, routine attribution across multiple products, derm escalation after two failed swaps, and a Shopify app with "Start a verified trial" on the PDP.

## Built with
go, preact, vite, sqlite, youcam-api, youcam-camera-kit, claude-code

## Demo data (being upfront)
- The "Demo: …" shoppers on the brand dashboard are synthetic score histories (`cmd/seed`), there so cohorts have something to show.
- "Demo: AI face" is one AI-generated face (thispersondoesnotexist.com, not a real person) saved as five selfies that differ only in crop, tilt and exposure, dated a week apart and run through the real YouCam API. Its *Not working* verdict and ₹699 credit are what the real scores produced.
- Live scans in the app are real YouCam calls.

## Links
- Code: https://github.com/manasnikam/skinverdict
- Demo video: <add YouTube/Vimeo link>
