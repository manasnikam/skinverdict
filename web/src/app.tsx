import { useEffect, useState } from "preact/hooks";
import { api, type Catalog, type Persona, type Trial, type TrialView, type Scan, type CohortRow } from "./api";

const ACTION_LABEL: Record<string, string> = { reorder: "Reordered", hold: "Holding", swap: "Swapped", refund_credit: "Proof Guarantee credit" };
const STATUS_LABEL: Record<string, string> = { working: "Working", not_yet: "Not yet", not_working: "Not working" };

// "People like you": outcomes for this product among shoppers with the same skin profile.
function Outcomes({ productId, profile, cat }: { productId: string; profile?: string; cat: Catalog }) {
  const [o, setO] = useState<{ all?: CohortRow; matching?: CohortRow } | null>(null);
  useEffect(() => { api.outcomes(productId, profile).then(setO).catch(() => setO(null)); }, [productId, profile]);
  const row = o?.matching ?? o?.all;
  if (!row || row.judged === 0) return <span class="small muted">No verified outcomes yet.</span>;
  const w = row.by_status.working ?? 0, nw = row.by_status.not_working ?? 0;
  const side = Object.entries(row.worsened ?? {}).map(([k, n]) => `${cat.concern_labels[k] ?? k} ×${n}`).join(", ");
  return (
    <span class="small">
      {o?.matching ? <b>{row.judged} {profile} shoppers</b> : <b>{row.judged} shoppers</b>}: {w} working, {nw} not · mean {row.mean_delta >= 0 ? "+" : ""}{row.mean_delta} on {cat.concern_labels[row.target] ?? row.target}
      {side && <span class="muted"> · flagged {side}</span>}
      {row.credits_issued > 0 && <span class="muted"> · {row.credits_issued} guarantee credit{row.credits_issued > 1 ? "s" : ""}</span>}
    </span>
  );
}
import { Camera } from "./camera";

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
  useEffect(() => { api.catalog().then(setCat).catch(console.error); }, []);
  if (!cat) return <main class="wrap"><p class="muted">Loading…</p></main>;

  let page;
  if (route.startsWith("/trial/")) page = <TrialPage id={route.slice(7)} cat={cat} />;
  else if (route === "/brand") page = <BrandPage cat={cat} />;
  else page = <Home cat={cat} />;

  return (
    <>
      <header class="top">
        <a href="#/" class="brand">SkinVerdict</a>
        <nav><a href="#/">Trials</a><a href="#/brand">Brand</a><Units /></nav>
      </header>
      <main class="wrap">{page}</main>
    </>
  );
}

function Units() {
  const [u, setU] = useState<number | null>(null);
  useEffect(() => { api.units().then((r) => setU(r.units)).catch(() => {}); }, []);
  return <span class="pill muted">{u == null ? "…" : `${u} units`}</span>;
}

// ---------- home: personas + trials + start trial ----------
function Home({ cat }: { cat: Catalog }) {
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [trials, setTrials] = useState<Trial[]>([]);
  const [name, setName] = useState("");
  const [pid, setPid] = useState("");
  const [prod, setProd] = useState(cat.products[0].id);
  const [err, setErr] = useState("");

  const load = () => Promise.all([api.personas(), api.trials()]).then(([p, t]) => { setPersonas(p); setTrials(t); if (!pid && p[0]) setPid(p[0].id); });
  useEffect(() => { load(); }, []);

  async function addPersona() {
    if (!name.trim()) return;
    const p = await api.createPersona(name.trim());
    setName(""); setPid(p.id); load();
  }
  async function start() {
    try { const t = await api.createTrial(pid, prod); go(`/trial/${t.id}`); }
    catch (e: any) { setErr(e.message); }
  }

  return (
    <>
      <section class="card">
        <h2>Start a trial</h2>
        <p class="muted">Buy a product, take a baseline scan, and we'll tell you — with numbers — whether it's working.</p>
        <div class="row">
          <select value={pid} onChange={(e) => setPid((e.target as HTMLSelectElement).value)}>
            {personas.map((p) => <option value={p.id}>{p.name}{p.profile ? ` · ${p.profile} skin` : ""}</option>)}
          </select>
          <input placeholder="new person" value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
          <button class="ghost" onClick={addPersona}>Add</button>
        </div>
        <div class="products">
          {cat.products.map((p) => (
            <label class={"product" + (prod === p.id ? " sel" : "")}>
              <input type="radio" name="prod" hidden checked={prod === p.id} onChange={() => setProd(p.id)} />
              <b>{p.name}</b><span class="muted">{p.brand} · ₹{p.price_inr}</span>
              <span class="small">{p.claim}</span>
              <span class="pill">judged on {cat.concern_labels[p.target] ?? p.target}</span>
              <Outcomes productId={p.id} profile={personas.find((x) => x.id === pid)?.profile} cat={cat} />
            </label>
          ))}
        </div>
        <button disabled={!pid} onClick={start}>Start trial →</button>
        {err && <p class="err">{err}</p>}
      </section>

      <section class="card">
        <h2>Active trials</h2>
        {trials.length === 0 && <p class="muted">None yet.</p>}
        <ul class="list">
          {trials.map((t) => {
            const p = cat.products.find((x) => x.id === t.product_id);
            const who = personas.find((x) => x.id === t.persona_id);
            return (
              <li><a href={`#/trial/${t.id}`}>
                <b>{who?.name ?? "?"}</b> · {p?.name} <span class={"pill st-" + t.status}>{t.status}</span>
              </a></li>
            );
          })}
        </ul>
      </section>
    </>
  );
}

// ---------- trial: scans, timeline, verdict, actions ----------
function TrialPage({ id, cat }: { id: string; cat: Catalog }) {
  const [v, setV] = useState<TrialView | null>(null);
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState("");
  const [at, setAt] = useState(""); // optional historical date for ingestion
  const [adh, setAdh] = useState("directed");
  const load = () => api.trial(id).then(setV).catch((e) => setErr(e.message));
  useEffect(() => { load(); }, [id]);

  async function scan(img: Blob) {
    setBusy("Analysing skin… (~6 s)"); setErr("");
    try { await api.scan(id, img, at ? new Date(at).toISOString() : undefined, v && v.scans.length > 0 ? adh : undefined); await load(); }
    catch (e: any) { setErr(e.message); }
    setBusy("");
  }
  async function checkin() {
    setBusy("Running check-in…");
    try { await api.checkin(id); await load(); } catch (e: any) { setErr(e.message); }
    setBusy("");
  }
  if (!v) return <p class="muted">{err || "Loading…"}</p>;
  const { trial, product, scans, verdict } = v;
  const label = (c: string) => cat.concern_labels[c] ?? c;

  return (
    <>
      <section class="card">
        <h2>{product.name} <span class="muted">· {product.brand}</span></h2>
        <p class="muted">Judged on <b>{label(trial.target)}</b>; also watching {trial.concerns.filter((c) => c !== trial.target).map(label).join(", ")}.</p>
        <div class={"verdict v-" + verdict.status}>
          <div class="big">{STATUS_LABEL[verdict.status]}</div>
          <div>{label(verdict.target)}: {verdict.baseline?.toFixed(1)} → {verdict.latest?.toFixed(1)}
            {" "}<b>({verdict.delta >= 0 ? "+" : ""}{verdict.delta?.toFixed(1)})</b> over {verdict.days} days · {verdict.scans} scans · confidence {verdict.confidence}</div>
          <p class="small">{verdict.explanation}</p>
        </div>
        {trial.last_action && (
          <div class="action">
            <b>Agent: {ACTION_LABEL[trial.last_action.kind] ?? trial.last_action.kind}
              {trial.last_action.credit_inr ? ` · ₹${trial.last_action.credit_inr} store credit` : ""}
              {trial.last_action.order_ref ? ` · ${trial.last_action.order_ref}` : ""}
              {trial.last_action.swap_to ? ` → ${cat.products.find((p) => p.id === trial.last_action!.swap_to)?.name}` : ""}</b>
            <p class="small">{trial.last_action.reason}</p>
          </div>
        )}
        <div class="row">
          <button class="ghost" onClick={checkin} disabled={!!busy}>Demo: run check-in now</button>
          <span class="muted small">next scheduled {new Date(trial.next_check).toLocaleString()}</span>
        </div>
      </section>

      <section class="card">
        <h2>{scans.length === 0 ? "Baseline scan" : `Scan #${scans.length + 1}`}</h2>
        {scans.length > 0 && (
          <div class="row">
            <label class="small muted">Used as directed since your last scan?
              <select value={adh} onChange={(e) => setAdh((e.target as HTMLSelectElement).value)}>
                <option value="directed">Yes, as directed</option>
                <option value="mostly">Mostly</option>
                <option value="skipped">Skipped it</option>
              </select>
            </label>
          </div>
        )}
        <Camera onCapture={scan} />
        <div class="row">
          <label class="small muted">Photo date (for importing older selfies)
            <input type="datetime-local" value={at} onInput={(e) => setAt((e.target as HTMLInputElement).value)} />
          </label>
        </div>
        {busy && <p class="muted">{busy}</p>}
        {err && <p class="err">{err}</p>}
      </section>

      {scans.length > 0 && (
        <section class="card">
          <h2>Timeline</h2>
          <Sparkline scans={scans} concern={trial.target} />
          <div class="scans">
            {scans.map((s, i) => (
              <div class="scan">
                <img src={s.mask_urls[trial.target] ?? s.image_url} alt="" />
                <div class="small">
                  <b>{i === 0 ? "Baseline" : `Day ${Math.round((+new Date(s.at) - +new Date(scans[0].at)) / 864e5)}`}</b> · {new Date(s.at).toLocaleDateString()}<br />
                  {trial.concerns.map((c) => <span>{label(c)} <b>{s.scores[c]?.toFixed(0)}</b> <span class="muted">(shown {s.ui_scores[c]?.toFixed(0)})</span><br /></span>)}
                  <span class="muted">{s.run_ms} ms · {s.cached ? "cached" : `${s.units} units`}{s.adherence === "skipped" ? " · skipped use, excluded" : ""}</span>
                </div>
              </div>
            ))}
          </div>
          <p class="muted small">Bold = raw_score (what the verdict uses). "Shown" = YouCam's consumer-facing ui_score.</p>
        </section>
      )}
    </>
  );
}

function Sparkline({ scans, concern }: { scans: Scan[]; concern: string }) {
  const W = 320, H = 80, P = 8;
  const t0 = +new Date(scans[0].at), t1 = +new Date(scans[scans.length - 1].at) || t0 + 1;
  const ys = scans.map((s) => s.scores[concern] ?? 0);
  const lo = Math.min(...ys) - 3, hi = Math.max(...ys) + 3;
  const pts = scans.map((s, i) => {
    const x = P + ((+new Date(s.at) - t0) / Math.max(1, t1 - t0)) * (W - 2 * P);
    const y = H - P - ((ys[i] - lo) / (hi - lo)) * (H - 2 * P);
    return [x, y];
  });
  return (
    <svg viewBox={`0 0 ${W} ${H}`} class="spark">
      <polyline fill="none" stroke="currentColor" stroke-width="2" points={pts.map((p) => p.join(",")).join(" ")} />
      {pts.map((p, i) => <><circle cx={p[0]} cy={p[1]} r="3" /><text x={p[0]} y={p[1] - 6} font-size="9" text-anchor="middle">{ys[i].toFixed(0)}</text></>)}
    </svg>
  );
}

// ---------- brand dashboard ----------
function BrandPage({ cat }: { cat: Catalog }) {
  const [rows, setRows] = useState<CohortRow[]>([]);
  const [byProfile, setByProfile] = useState(false);
  useEffect(() => { api.cohorts(byProfile).then(setRows); }, [byProfile]);
  return (
    <section class="card">
      <h2>Brand dashboard</h2>
      <p class="muted">Does each SKU actually move the concern it claims to, across real customers' scans?</p>
      <div class="row">
        <label class="small"><input type="checkbox" checked={byProfile} onChange={(e) => setByProfile((e.target as HTMLInputElement).checked)} /> split by skin profile</label>
      </div>
      <table>
        <thead><tr><th>Product</th>{byProfile && <th>Profile</th>}<th>Judged on</th><th>Trials</th><th>Working</th><th>Not yet</th><th>Not working</th><th>Mean Δ</th><th>Side effects</th><th>Credits</th></tr></thead>
        <tbody>
          {rows.map((r) => (
            <tr>
              <td>{r.name}</td>{byProfile && <td>{r.profile}</td>}<td>{cat.concern_labels[r.target] ?? r.target}</td><td>{r.trials}</td>
              <td class="ok">{r.by_status.working ?? 0}</td><td>{r.by_status.not_yet ?? 0}</td><td class="bad">{r.by_status.not_working ?? 0}</td>
              <td>{r.mean_delta}</td>
              <td class="small">{Object.entries(r.worsened ?? {}).map(([k, n]) => `${cat.concern_labels[k] ?? k} ×${n}`).join(", ") || "—"}</td>
              <td>{r.credits_issued || "—"}</td>
            </tr>
          ))}
          {rows.length === 0 && <tr><td colSpan={10} class="muted">No trials yet.</td></tr>}
        </tbody>
      </table>
    </section>
  );
}
