import { useEffect, useState } from "preact/hooks";
import { api, type Catalog, type Persona, type Trial, type TrialView, type Scan, type CohortRow } from "./api";
import { Camera } from "./camera";
import { Icon } from "./icons";

declare global { interface Window { svTheme?: (t: string, save?: boolean) => void } }

const VERDICT = { working: ["Working", "checkCircle"], not_yet: ["Too early to tell", "clock"], not_working: ["Not working", "xCircle"] } as const;
const STATUS: Record<string, [string, string]> = { active: ["In progress", "clock"], reordered: ["Renewed", "repeat"], swapped: ["Switched", "swap"], credited: ["Refunded as credit", "tag"] };
const CONF: Record<string, string> = { low: "Early read", medium: "Solid", high: "Final read" };
const ADH: Record<string, string> = { directed: "Yes", mostly: "Mostly", skipped: "Skipped (excluded)" };
const inr = (n: number) => "₹" + n.toLocaleString("en-IN");
const NOISE = 4; // matches verdict.DefaultOptions().NoiseBand
const pct = (a: number, b: number) => (b ? Math.round((100 * a) / b) : 0);
const dayOf = (s: Scan, s0: Scan) => Math.round((+new Date(s.at) - +new Date(s0.at)) / 864e5);
const cost = (s: Scan) => (s.cached ? "Replayed from cache · 0 units (same image)" : `${(s.run_ms / 1000).toFixed(1)} s · ${s.units} units`);
const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);
const StatusPill = ({ s }: { s: string }) => { const [w, i] = STATUS[s] ?? [s, "clock"]; return <span class={"pill s-" + s}><Icon n={i} />{w}</span>; };
function StackedBar({ w, y, n }: { w: number; y: number; n: number }) {
  return <div class="bar" role="img" aria-label={`${w} working, ${y} too early to tell, ${n} not working`}>
    {w > 0 && <i class="w" style={{ flexGrow: w }} />}{y > 0 && <i class="y" style={{ flexGrow: y }} />}{n > 0 && <i class="n" style={{ flexGrow: n }} />}
  </div>;
}
const Alert = ({ msg, retry }: { msg: string; retry?: () => void }) =>
  <div class="alert" role="alert"><Icon n="alert" />{msg}{retry && <button onClick={retry}>Retry</button>}</div>;

// "People like you": outcomes for this product among shoppers with the same skin profile.
function Outcomes({ productId, profile, cat }: { productId: string; profile?: string; cat: Catalog }) {
  const [o, setO] = useState<{ all?: CohortRow; matching?: CohortRow } | null>(null);
  useEffect(() => { api.outcomes(productId, profile).then(setO).catch(() => setO(null)); }, [productId, profile]);
  const row = o?.matching ?? o?.all;
  if (!row || row.judged === 0) return <p class="t-sub">No verified results yet.</p>;
  const w = row.by_status.working ?? 0, nw = row.by_status.not_working ?? 0, ny = row.by_status.not_yet ?? 0;
  const worst = Object.entries(row.worsened ?? {}).sort((a, b) => b[1] - a[1])[0];
  const label = (c: string) => cat.concern_labels[c] ?? c;
  return (
    <div style="display:grid;gap:6px">
      <span class="t-foot">People like you</span>
      <p><b>{w} of {row.judged}</b> {o?.matching && profile ? `${profile}-skin shoppers` : "shoppers"}: working</p>
      <StackedBar w={w} y={ny} n={nw} />
      <p class="t-foot">{nw} not working · {ny} too early to tell</p>
      {worst && <p class="t-sub">{label(worst[0])} got worse for {worst[1]} of {row.judged}</p>}
    </div>
  );
}

// ---------- tiny hash router ----------
function useRoute() {
  const [h, setH] = useState(location.hash.slice(1) || "/");
  useEffect(() => {
    const f = () => setH(location.hash.slice(1) || "/");
    addEventListener("hashchange", f);
    return () => removeEventListener("hashchange", f);
  }, []);
  return h;
}
const go = (p: string) => (location.hash = p);

export function App() {
  const route = useRoute();
  const [cat, setCat] = useState<Catalog | null>(null);
  const [catErr, setCatErr] = useState(false);
  const loadCat = () => { setCatErr(false); api.catalog().then(setCat).catch(() => setCatErr(true)); };
  useEffect(loadCat, []);
  if (!cat) return <main class="wrap">{catErr ? <Alert msg="Couldn't load the catalog." retry={loadCat} /> : <p class="t-sub">Loading…</p>}</main>;

  let page;
  if (route.startsWith("/trial/")) page = <TrialPage id={route.slice(7)} cat={cat} />;
  else if (route === "/brand") page = <BrandPage cat={cat} />;
  else page = <Home cat={cat} />;
  const here = (r: string) => (route === r ? "page" : undefined);

  return (
    <>
      <header class="top">
        <a href="#/" class="brand">SkinVerdict</a>
        <nav class="tabs" aria-label="Main">
          <a href="#/" aria-current={route === "/" || route.startsWith("/trial/") ? "page" : undefined}><Icon n="list" cls="lg" />Trials</a>
          <a href="#/brand" aria-current={here("/brand")}><Icon n="chart" cls="lg" />Brand</a>
        </nav>
      </header>
      <main class={"wrap" + (route === "/brand" ? " wide" : "")}>{page}<Footer /></main>
    </>
  );
}

function Footer() {
  const [u, setU] = useState<number | null>(null);
  useEffect(() => { api.units().then((r) => setU(r.units)).catch(() => {}); }, []);
  return (
    <footer class="foot">
      <p>Powered by YouCam AI Skin Analysis · JS Camera Kit</p>
      <p>{u ?? "–"} API units left</p>
      <Appearance />
    </footer>
  );
}

function Appearance() {
  const [t, setT] = useState(document.documentElement.dataset.theme || "light");
  return (
    <div class="seg" role="radiogroup" aria-label="Appearance">
      {[["light", "iOS Light"], ["dark", "iOS Dark"], ["retro", "Retro"]].map(([v, l]) => (
        <label><input type="radio" class="sr" name="theme" checked={t === v} onChange={() => { window.svTheme?.(v); setT(v); }} />{l}</label>
      ))}
    </div>
  );
}

// ---------- home: personas + trials + start trial ----------
function Home({ cat }: { cat: Catalog }) {
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [trials, setTrials] = useState<Trial[]>([]);
  const [name, setName] = useState("");
  const [adding, setAdding] = useState(false);
  const [pid, setPid] = useState("");
  const [prod, setProd] = useState(cat.products[0].id);
  const [err, setErr] = useState("");
  const label = (c: string) => cat.concern_labels[c] ?? c;
  const product = cat.products.find((x) => x.id === prod)!;
  const profile = personas.find((x) => x.id === pid)?.profile;

  const load = () => Promise.all([api.personas(), api.trials()])
    .then(([p, t]) => { setPersonas(p); setTrials(t); if (!pid && p[0]) setPid(p[0].id); })
    .catch((e) => setErr(e.message));
  useEffect(() => { load(); }, []);

  async function addPersona() {
    if (!name.trim()) return;
    const p = await api.createPersona(name.trim());
    setName(""); setPid(p.id); setAdding(false); load();
  }
  async function start() {
    try { const t = await api.createTrial(pid, prod); go(`/trial/${t.id}`); }
    catch (e: any) { setErr(e.message); }
  }

  return (
    <>
      <div style="display:grid;gap:4px">
        <h1 class="t-large">Start a trial</h1>
        <p class="t-sub">We scan your skin before and after, and tell you if it's working.</p>
      </div>

      <h2 class="t-head">Who's trying it</h2>
      <div class="chips" role="radiogroup" aria-label="Who's trying it">
        {personas.map((p) => (
          <label class="chip"><input type="radio" class="sr" name="who" checked={pid === p.id} onChange={() => setPid(p.id)} />
            <span class="avatar">{p.name[0]}</span>
            <span><b>{p.name}</b><br /><span class="t-sub">{p.profile ? `${cap(p.profile)} skin` : "Skin type after scan"}</span></span>
            {pid === p.id && <Icon n="check" />}</label>
        ))}
        <button class="chip tinted" onClick={() => setAdding(true)}><Icon n="plus" />New</button>
      </div>
      {adding && <div class="row" style="flex-wrap:nowrap">
        <input placeholder="Name" aria-label="Shopper name" value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)}
          onKeyDown={(e) => { if (e.key === "Enter") addPersona(); }} />
        <button onClick={addPersona}>Add</button>
      </div>}

      <h2 class="t-head">Pick a product</h2>
      <div class="products" role="radiogroup" aria-label="Product">
        {cat.products.map((p) => (
          <label class="product">
            <input type="radio" name="prod" class="sr" checked={prod === p.id} onChange={() => setProd(p.id)} />
            <span class="radio">{prod === p.id && <Icon n="check" />}</span>
            <b class="t-head">{p.name}</b>
            <span class="t-sub">{p.brand} · {inr(p.price_inr)}</span>
            <span>{p.claim}</span>
            <span class="pill">Checks: {label(p.target)}</span>
            <Outcomes productId={p.id} profile={profile} cat={cat} />
          </label>
        ))}
      </div>
      <button class="block sticky-cta" disabled={!pid} onClick={start} aria-label={`Start trial of ${product.name}`}>Start trial · {inr(product.price_inr)}</button>
      {err && <Alert msg={err} />}

      <h2 class="t-head">Your trials</h2>
      {trials.length === 0 ? <p class="t-sub">No trials yet. Pick a product above.</p> : (
        <ul class="list">
          {trials.map((t) => {
            const p = cat.products.find((x) => x.id === t.product_id);
            const who = personas.find((x) => x.id === t.persona_id);
            return (
              <li><a href={`#/trial/${t.id}`}>
                <span class="avatar">{who?.name[0] ?? "?"}</span>
                <span class="who"><b>{who?.name ?? "Shopper"}</b><span class="t-sub">{p?.name}</span><StatusPill s={t.status} /></span>
                <Icon n="chevR" cls="chev" />
              </a></li>
            );
          })}
        </ul>
      )}
    </>
  );
}

// ---------- trial: verdict, agent action, scans, timeline ----------
const ACTION: Record<string, [string, string]> = { reorder: ["reordered", "repeat"], hold: ["active", "play"], swap: ["swapped", "swap"], refund_credit: ["credited", "tag"] };

function TrialPage({ id, cat }: { id: string; cat: Catalog }) {
  const [v, setV] = useState<TrialView | null>(null);
  const [who, setWho] = useState<Persona | undefined>();
  const [busy, setBusy] = useState("");
  const [done, setDone] = useState("");
  const [err, setErr] = useState("");
  const [at, setAt] = useState(""); // optional historical date for ingestion
  const [adh, setAdh] = useState("directed");
  const load = () => Promise.all([api.trial(id), api.personas()])
    .then(([tv, ps]) => { setV(tv); setWho(ps.find((p) => p.id === tv.trial.persona_id)); })
    .catch((e) => setErr(e.message));
  useEffect(() => { load(); }, [id]);

  async function scan(img: Blob) {
    setBusy("Analysing with YouCam AI Skin Analysis…"); setDone(""); setErr("");
    try {
      const s = await api.scan(id, img, at ? new Date(at).toISOString() : undefined, v && v.scans.length > 0 ? adh : undefined);
      setDone(s.cached ? cost(s) : "Analysed in " + cost(s));
      await load();
    } catch (e: any) { setErr(e.message); }
    setBusy("");
  }
  async function checkin() {
    setBusy("Running check-in…"); setDone("");
    try { await api.checkin(id); await load(); } catch (e: any) { setErr(e.message); }
    setBusy("");
  }
  if (!v) return err ? <Alert msg={err} retry={() => { setErr(""); load(); }} /> : <p class="t-sub">Loading…</p>;
  const { trial, product, scans, verdict } = v;
  const label = (c: string) => cat.concern_labels[c] ?? c;
  const closed = trial.status === "credited" || trial.status === "swapped";
  const sign = (n: number) => (n >= 0 ? "+" : "") + n.toFixed(1);
  const target = trial.target, Label = label(target);
  const guards = trial.concerns.filter((c) => c !== target && cat.concern_labels[c]).map(label);
  const shown = trial.concerns.filter((c) => cat.concern_labels[c]);
  const days = verdict.days;
  const a = trial.last_action;
  const alt = a?.swap_to ? cat.products.find((p) => p.id === a.swap_to) : undefined;
  const s0 = scans[0], sN = scans[scans.length - 1];

  return (
    <>
      <div style="display:grid;gap:4px">
        <a class="back" href="#/"><Icon n="chevL" />Trials</a>
        <h1 class="t-title">{product.name}</h1>
        <p class="t-sub">{product.brand}{who ? ` · ${who.name}` : ""}{who?.profile ? ` · ${cap(who.profile)} skin (YouCam skin_type)` : ""}</p>
        <p class="t-sub">Judged on <b>{Label}</b>.{guards.length > 0 && ` Also watching ${guards.join(", ")}.`}</p>
      </div>

      <section key={scans.length ? verdict.status : "none"} class={"verdict " + (scans.length ? "v-" + verdict.status : "v-none")} role="status">
        {scans.length === 0 ? <>
          <p class="word"><Icon n="camera" cls="xl" />Take your Day 0 scan</p>
          <p class="sub">We compare every check-in to this first scan.</p>
        </> : <>
          <p class="word"><Icon n={VERDICT[verdict.status][1]} cls="xl" />{VERDICT[verdict.status][0]}</p>
          <p class="line">{Math.abs(verdict.delta) < NOISE
            ? `${Label} barely moved (${verdict.delta > 0 ? "+" : ""}${verdict.delta.toFixed(1)}) in ${days} days: no real change`
            : `${Label} ${verdict.delta > 0 ? "improved" : "got worse"} ${Math.abs(verdict.delta).toFixed(1)} points in ${days} days`}</p>
          <p class="sub">First scan {verdict.baseline.toFixed(1)} → latest {verdict.latest.toFixed(1)} · higher = healthier</p>
          <span class="chip-w">{CONF[verdict.confidence] ?? verdict.confidence}</span>
          <div class="progress" role="progressbar" aria-valuemin={0} aria-valuemax={28} aria-valuenow={Math.min(days, 28)} aria-label="Trial progress"><i style={{ width: Math.min(100, (days / 28) * 100) + "%" }} /></div>
          <p class="sub">{days >= 28 ? "Day 28 reached" : `Day ${days} of 28 · ${28 - days} days to go`}</p>
        </>}
      </section>

      {a && (
        <section class={"card receipt s-" + (ACTION[a.kind]?.[0] ?? "active")}>
          <span class="badge"><Icon n={ACTION[a.kind]?.[1] ?? "play"} /></span>
          <p class="t-head">{{ reorder: "Reordered for you", hold: "Keep going", swap: `Switched you to ${alt?.name}`, refund_credit: "Proof Guarantee: store credit" }[a.kind]}</p>
          {a.kind === "refund_credit" && <p class="stat">{inr(a.credit_inr!)} credit</p>}
          {a.kind === "reorder" && <p class="stat">{inr(product.price_inr)} order</p>}
          {a.kind === "refund_credit" && alt && <p>Next: {alt.name}</p>}
          <p>{a.reason}</p>
          <p class="t-foot">{a.order_ref ? `Order ${a.order_ref} · ` : ""}{new Date(a.at).toLocaleDateString()}</p>
        </section>
      )}

      {closed ? <section class="card"><p class="t-head"><Icon n="check" /> Trial closed. No more check-ins.</p></section> : (
        <section class="card">
          <h2 class="t-head">{scans.length ? `Check-in · Scan ${scans.length + 1}` : "Day 0 scan (baseline)"}</h2>
          {scans.length > 0 && <>
            <p id="adh-l" class="t-sub">Used it as directed since your last scan?</p>
            <div class="seg" role="radiogroup" aria-labelledby="adh-l">
              {[["directed", "Yes"], ["mostly", "Mostly"], ["skipped", "Skipped"]].map(([k, l]) => (
                <label><input type="radio" class="sr" name="adh" checked={adh === k} onChange={() => setAdh(k)} />{l}</label>
              ))}
            </div>
          </>}
          <Camera onCapture={scan} disabled={!!busy} />
          {busy ? <p class="note" role="status"><span class="spin" aria-hidden="true" />{busy}</p>
            : done && <p class="note" role="status"><Icon n="checkCircle" />{done}</p>}
          {err && <Alert msg={err} />}
          <p class="t-sub">Next check-in {new Date(trial.next_check).toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" })}</p>
          <button class="plain" onClick={checkin} disabled={!!busy}>Simulate next check-in (demo)</button>
        </section>
      )}

      {scans.length > 0 && <>
        <section class="card">
          <h2 class="t-head">{Label} skin score</h2>
          <Sparkline scans={scans} concern={target} base={verdict.baseline} />
          <p class="t-sub">Shaded band: changes under 4 points count as no change. Dashed line: Day 28 deadline.</p>
        </section>

        <section class="card">
          <h2 class="t-head">Before and after · YouCam {Label} mask</h2>
          <div class="ba">
            {(scans.length > 1 ? [s0, sN] : [s0]).map((s) => (
              <figure>
                <img src={s.mask_urls[target] ?? s.image_url} alt={`${Label} mask, day ${dayOf(s, s0)}`} />
                <b class="t-sub">{s === s0 ? "Day 0" : `Day ${dayOf(sN, s0)}`}</b>
                <span class="t-foot">YouCam raw score</span>
                <span class="stat">{Math.round(s.scores[target])}</span>
                <span>App shows {Math.round(s.ui_scores[target])}</span>
                <span class="t-foot">{cost(s)}</span>
              </figure>
            ))}
          </div>
          <p class="t-sub">We judge on the raw score. The app score is the friendlier number YouCam shows shoppers.</p>
          {scans.length > 1 && Object.keys(verdict.worsened ?? {}).filter((k) => s0.mask_urls[k]).map((k) => <>
            <p class="t-head">{label(k)} got worse</p>
            <div class="ba small">
              <img src={s0.mask_urls[k]} alt={`${label(k)} mask, day 0`} />
              <img src={sN.mask_urls[k] ?? sN.image_url} alt={`${label(k)} mask, day ${dayOf(sN, s0)}`} />
            </div>
          </>)}
          <div class="api"><b>Analysed by YouCam AI Skin Analysis · quality-gated by JS Camera Kit</b><span>Last scan: {cost(sN)}</span></div>
        </section>

        <details class="how">
          <summary>How we judged <Icon n="chevR" /></summary>
          <div>
            <p>{verdict.explanation}</p>
            <dl class="kv">
              <dt>Start → latest</dt><dd>{verdict.baseline.toFixed(1)} → {verdict.latest.toFixed(1)} (YouCam raw score)</dd>
              <dt>Change</dt><dd>{sign(verdict.delta)}</dd>
              <dt>Trend</dt><dd>{verdict.slope_per_wk.toFixed(1)} points per week</dd>
              <dt>Confidence</dt><dd>{CONF[verdict.confidence] ?? verdict.confidence}</dd>
              <dt>Rule</dt><dd>Changes under 4 points count as no change. Day 28 is the deadline.</dd>
            </dl>
            <div class="table-wrap">
              <table>
                <thead><tr><th>Date</th><th class="num">Day</th>{shown.map((c) => <th class="num">{label(c)} raw / app</th>)}<th>Used as directed</th><th class="num">Time</th><th class="num">Units</th></tr></thead>
                <tbody>
                  {scans.map((s, i) => (
                    <tr>
                      <td>{new Date(s.at).toLocaleDateString()}</td>
                      <td class="num">{dayOf(s, s0)}</td>
                      {shown.map((c) => <td class="num">{Math.round(s.scores[c])} / {Math.round(s.ui_scores[c])}</td>)}
                      <td>{i === 0 ? "–" : ADH[s.adherence] ?? "–"}</td>
                      <td class="num">{(s.run_ms / 1000).toFixed(1)} s</td>
                      <td class="num">{s.cached ? "0 (replayed)" : s.units}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {!closed && <label class="field">Import an older photo: set its date, then scan or upload
              <input type="datetime-local" value={at} onInput={(e) => setAt((e.target as HTMLInputElement).value)} />
            </label>}
          </div>
        </details>
      </>}
    </>
  );
}

function Sparkline({ scans, concern, base }: { scans: Scan[]; concern: string; base: number }) {
  const W = 340, H = 150, PL = 8, PR = 8, PT = 24, PB = 22;
  const t0 = +new Date(scans[0].at);
  const d = (s: Scan) => (+new Date(s.at) - t0) / 864e5;
  const xmax = Math.max(28, d(scans[scans.length - 1]));
  const ys = scans.map((s) => s.scores[concern] ?? 0);
  const lo = Math.min(...ys, base - 4) - 3, hi = Math.max(...ys, base + 4) + 3;
  const x = (v: number) => PL + (v / xmax) * (W - PL - PR);
  const y = (v: number) => PT + ((hi - v) / (hi - lo)) * (H - PT - PB);
  const pts = scans.map((s, i) => [x(d(s)), y(ys[i])]);
  return (
    <svg viewBox={`0 0 ${W} ${H}`} class="spark" role="img" aria-label={`${concern} raw score per scan: ${ys.map((v) => v.toFixed(0)).join(", ")}`}>
      <rect class="band" x={PL} width={W - PL - PR} y={y(base + 4)} height={y(base - 4) - y(base + 4)} />
      <line class="base" x1={PL} x2={W - PR} y1={y(base)} y2={y(base)} />
      <line class="hz" x1={x(28)} x2={x(28)} y1={PT - 8} y2={H - PB} />
      <text class="lbl" x={PL} y={H - 4}>Day 0</text>
      <text class="lbl" x={x(28)} y={H - 4} text-anchor="end">Day 28</text>
      <polyline points={pts.map((p) => p.join(",")).join(" ")} />
      {pts.map((p) => <circle cx={p[0]} cy={p[1]} r="4" />)}
      {pts.map((p, i) => <text x={p[0]} y={p[1] - 10} text-anchor="middle">{Math.round(ys[i])}</text>)}
    </svg>
  );
}

// ---------- brand dashboard ----------
const sum = (a: CohortRow[], f: (r: CohortRow) => number) => a.reduce((s, r) => s + f(r), 0);
const sh = (r: CohortRow, k: string) => (r.judged ? (r.by_status[k] ?? 0) / r.judged : -1);

function BrandPage({ cat }: { cat: Catalog }) {
  const [all, setAll] = useState<CohortRow[] | null>(null);
  const [split, setSplit] = useState<CohortRow[]>([]);
  const [trials, setTrials] = useState<Trial[]>([]);
  const [view, setView] = useState<"profile" | "all">("profile");
  const [err, setErr] = useState("");
  // ponytail: split rows fetched up front with the rest (default view needs them anyway)
  const load = () => { setErr(""); Promise.all([api.cohorts(false), api.cohorts(true), api.trials()])
    .then(([a, s, t]) => { setAll(a); setSplit(s); setTrials(t); }).catch((e) => setErr(e.message)); };
  useEffect(load, []);
  if (err) return <Alert msg={err} retry={load} />;
  if (!all) return <p class="t-sub">Loading…</p>;

  const label = (c: string) => cat.concern_labels[c] ?? c;
  const price = (id: string) => cat.products.find((p) => p.id === id)?.price_inr ?? 0;
  const J = sum(all, (r) => r.judged), W = sum(all, (r) => r.by_status.working ?? 0), T = sum(all, (r) => r.trials);
  const C = sum(all, (r) => r.credits_inr), rev = sum(all, (r) => r.trials * price(r.product_id));
  const renewed = trials.filter((t) => t.status === "reordered"), R = renewed.reduce((a, t) => a + price(t.product_id), 0);
  const prof = view === "profile";
  const rows = [...(prof ? split : all)].sort((a, b) => sh(b, "not_working") - sh(a, "not_working") || sh(a, "working") - sh(b, "working") || a.name.localeCompare(b.name));
  const kpis: [string, string | number, string][] = [
    ["Success rate", `${pct(W, J)}%`, `${W} of ${J} judged trials working`],
    ["Proof Guarantee credits", inr(C), `${pct(C, rev)}% of trial revenue`],
    ["Reorder revenue", inr(R), `${renewed.length} renewals`],
    ["Trials", T, `${J} with a verdict`],
  ];

  return (
    <>
      <div style="display:grid;gap:4px">
        <h1 class="t-large">Brand insights</h1>
        <p class="t-sub">Which product fails for which skin type, and what it costs you.</p>
      </div>
      <div class="kpis">
        {kpis.map(([l, v, s]) => <div class="card kpi"><span class="t-sub">{l}</span><span class="stat xl">{v}</span><span class="t-foot">{s}</span></div>)}
      </div>
      <div class="row" style="justify-content:space-between;gap:12px">
        <div class="seg" role="radiogroup" aria-label="Group results" style="flex:0 1 360px">
          {([["profile", "By skin type"], ["all", "All shoppers"]] as const).map(([k, l]) => (
            <label><input type="radio" class="sr" name="view" checked={view === k} onChange={() => setView(k)} />{l}</label>
          ))}
        </div>
        <div class="legend">
          <span><i style="background:var(--work)" />Working</span>
          <span><i style="background:var(--yet)" />Too early to tell</span>
          <span><i style="background:var(--bad)" />Not working</span>
        </div>
      </div>
      <div class="table-wrap">
        <table class="brand-table">
          <thead><tr><th>Product</th>{prof && <th>Skin type</th>}<th class="num">Trials</th><th>Results</th><th class="num">Not yet</th><th class="num">Avg change</th><th>Side effects</th><th class="num">Credits</th></tr></thead>
          <tbody>
            {rows.map((r) => {
              const w = r.by_status.working ?? 0, nw = r.by_status.not_working ?? 0, ny = r.by_status.not_yet ?? 0;
              return (
                <tr>
                  <td>
                    <b>{r.name}</b>
                    <div class="t-foot">Checks: {label(r.target)}</div>
                    {prof && r.judged > 0 && nw / r.judged >= 0.5 && <span class="pill v-not_working"><Icon n="xCircle" />Fails for {r.profile} skin · {nw}/{r.judged}</span>}
                  </td>
                  {prof && <td>{cap(r.profile ?? "–")}</td>}
                  <td class="num">{r.trials}</td>
                  <td style="min-width:160px">{r.judged ? <div style="display:grid;gap:6px"><StackedBar w={w} y={ny} n={nw} /><span>{pct(w, r.judged)}% working</span></div> : "No results yet"}</td>
                  <td class="num q">{ny}</td>
                  <td class="num q">{r.mean_delta > 0 ? "+" : ""}{r.mean_delta}</td>
                  <td>{Object.entries(r.worsened ?? {}).map(([k, n]) => `${label(k)} ${pct(n, r.judged)}%`).join(", ") || "None"}</td>
                  <td class="num">{r.credits_issued ? inr(r.credits_inr) : "–"}<span class="t-foot"> ({r.credits_issued})</span></td>
                </tr>
              );
            })}
            {rows.length === 0 && <tr><td colSpan={prof ? 8 : 7} class="q">No trials yet.</td></tr>}
          </tbody>
        </table>
      </div>
    </>
  );
}
