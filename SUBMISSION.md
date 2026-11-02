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

## When the API surprised us
**The frustrating one.** On the last day, YouCam's skin analysis normally finished in about 6 seconds, then suddenly took 2 minutes 40 seconds per task. Our client gave up polling after 3 minutes, but the tasks still finished on YouCam's side and charged their units. Two scans were paid for and we got no result. We raised the poll timeout to 8 minutes, and the next batch ran at normal speed. The lesson: for a paid async API, a client-side timeout doesn't cancel anything, so wait long enough to collect what you've already paid for.

**The surprising one, which changed the product.** To build a demo timeline, I took one face and made five "selfies" from it, changing only the crop, a 1° tilt and the exposure. The skin itself was never touched. YouCam scored the pores 91, 97, 81, 96 and 93. That's a 16-point spread on a face that hadn't changed at all. Our verdict had compared the first scan with the latest one, so if the 81 had happened to be the last photo, we would have told someone their serum was hurting them and refunded the wrong product. We rewrote the rule: with three or more scans, SkinVerdict now counts only the change that both the endpoints and a robust trend line agree on, using a Theil–Sen median slope. That way one bad photo can't decide a refund. The surprise turned into the line we close the demo on: "One scan can't decide a refund. Five can."

## Where we hit a wall, and the workaround
1. **You can't demo a 4-week product in 48 hours.** The verdict needs weeks of scans of one person, and I didn't have dated selfies. The workaround has three parts, each labelled in the app and the README:
   - an ingest tool (`cmd/ingest`) that feeds photos into the real scan pipeline with a date per photo;
   - one AI-generated face, saved as five "selfies" that differ only in framing and exposure, run through the real YouCam API;
   - seeded synthetic shoppers so the brand dashboard has cohorts to show.

   The verdicts and credits you see on the AI face are what YouCam's real scores produced. Nothing was edited to force a result.
2. **My first fix for scan noise didn't work.** When one unchanged face scored 81–97, my first instinct was to judge a least-squares trend line instead of two endpoints. I wrote a test with a single bad last scan, and the test failed: least squares gets dragged by an outlier at the end, too. Averaging the first two and last two scans would have flipped a correct verdict on existing data. What held up was a Theil–Sen trend (the median of all pairwise slopes), and counting a change only when it agrees with first-vs-latest. Before shipping, I re-ran every existing trial to confirm no verdict changed status.
3. **Camera Kit is hard to test.** It needs HTTPS and a real camera, so it won't run on `localhost` over plain HTTP or in a headless browser, and the SDK's quality events (`area` vs the older `position` field) differ by version. I made the SDK announce itself through `window.ymkAsyncInit`, mapped both field names, kept a clearly labelled "Upload photo · not quality-checked" fallback, and tested the real camera on an iPhone through an HTTPS tunnel.
4. **Masks expire.** YouCam returns mask overlays as presigned URLs that expire in about 2 hours, so a before/after view built on them would break by the next check-in. We download and store each mask at scan time.
5. **Letting an AI agent touch money.** A model deciding refunds is risky. The verdict is computed first by deterministic, unit-tested code and is final. The agent (Claude Code, run headlessly) can only choose among the actions that verdict allows and write the explanation. If it answers off-policy, returns malformed JSON, or the CLI fails, a rule-based agent takes over, so a check-in never blocks. Testing also caught a real exploit: checking in again on an already-credited trial could issue a second credit. Check-ins on closed trials are now a no-op, with a test.

## Use cases nobody is talking about yet
Most people treat skin analysis as a one-time thing: scan once, get a product recommendation. Building SkinVerdict convinced me the bigger value is in **repeat measurement**: the same person scanned the same way over weeks, with statistics that account for scan noise. Seen that way, a few industries open up:

1. **Cosmetic claims substantiation.** "Visibly reduces pores in 4 weeks" usually needs a lab panel: recruit people, bring them in weekly, measure. A remote panel, where participants scan at home through a capture gate like Camera Kit and robust trend statistics are run on the results, could make claim testing faster and cheaper. That would let small D2C brands substantiate claims they currently can't afford to test.
2. **Retail returns and guarantees.** Opened skincare usually can't be resold, so "money-back if it doesn't work" is either a pure loss or a promise brands quietly avoid. A measured outcome turns it into a policy a retailer can actually price: refund when the scans say it didn't work, renew when they say it did. That's what SkinVerdict does, and the same approach fits pharmacies and marketplaces.
3. **Follow-up between dermatologist visits.** For treatments that take months to show results (acne regimens, pigmentation treatments), patients often quit before anything visible happens. Weekly at-home scans could show a doctor the trend before the next appointment. This would be tracking, not diagnosis, but it addresses why people abandon treatment.
4. **Salons, spas and clinics proving their results.** A facial or peel package is sold on before/after photos that customers don't fully trust. Standardised scans with raw scores give service businesses evidence they can show a client at session six.
5. **Checking influencer "30-day results".** Brands pay creators for transformation content they can't verify. A scan series with a quality gate and dated captures is much harder to fake than two photos.

The common thread came from our own data: five photos of one unchanged face scored between 81 and 97 on pores. The API is accurate enough to be useful, but any single score is noisy. The products that win will treat YouCam as a measuring instrument and design around its error margin, rather than treating each score as a final answer.

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
- Live demo: https://skinverdict-gwjm7k5b2a-uc.a.run.app
- Code: https://github.com/manasnikam/skinverdict
- Demo video: https://youtu.be/RpIzEpALbzY

## Gallery
Images are exported from the demo video at 1500×1000 (3:2) into `video/out/gallery/` (gitignored; regenerate from `video/out/final.mp4`). Use image 1 as the cover. YouTube thumbnail: `video/out/youtube-thumbnail.jpg` (1280×720).

| # | File | Caption |
|---|---|---|
| 1 | `01-proof-guarantee.jpg` | Not working after 28 days: the Proof Guarantee settles itself as ₹699 store credit and suggests a swap. |
| 2 | `02-pick-a-product.jpg` | Pick a product and see how it worked for people with your skin type. |
| 3 | `03-youcam-pore-mask.jpg` | YouCam AI Skin Analysis scores each concern and maps where it shows up. |
| 4 | `04-working-reorder.jpg` | Working: pores up 9.8 points in 28 days, outside the ±4 noise band, so the agent reorders. |
| 5 | `05-raw-vs-app-score.jpg` | We judge YouCam's raw score and also show the friendlier app score. |
| 6 | `06-one-bad-scan.jpg` | Same face, one bad scan: why a single score can't decide a refund. |
| 7 | `07-how-we-judged.jpg` | First to latest said +2.2, the robust trend said +0.1; we count only what both agree on. |
| 8 | `08-brand-dashboard.jpg` | Brand view: Niacinamide fails for dry skin 2 of 2, and what it cost in credits. |
| 9 | `09-under-the-hood.jpg` | Real code: the Theil–Sen trend that keeps one outlier scan from deciding a refund. |
