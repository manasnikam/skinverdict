export type Product = {
  id: string; name: string; brand: string; price_inr: number; claim: string;
  target: string; guards: string[]; alternatives: string[];
};
export type Catalog = { products: Product[]; concern_labels: Record<string, string> };
export type Persona = { id: string; name: string; profile?: string };
export type Action = { at: string; kind: "reorder" | "hold" | "swap" | "refund_credit"; reason: string; swap_to?: string; order_ref?: string; credit_inr?: number };
export type Trial = {
  id: string; persona_id: string; product_id: string; target: string; concerns: string[];
  started_at: string; next_check: string; status: string; last_action?: Action;
};
export type Scan = {
  id: string; at: string; scores: Record<string, number>; ui_scores: Record<string, number>;
  overall: number; skin_age: number; image_url: string; mask_urls: Record<string, string>;
  run_ms: number; units: number; cached: boolean; adherence: string;
};
export type CohortRow = {
  product_id: string; name: string; target: string; profile?: string; trials: number; judged: number;
  by_status: Record<string, number>; mean_delta: number; worsened: Record<string, number>; credits_issued: number; credits_inr: number;
};
export type Verdict = {
  status: "working" | "not_yet" | "not_working"; target: string; baseline: number; latest: number;
  delta: number; slope_per_wk: number; days: number; scans: number; worsened?: Record<string, number>;
  confidence: string; explanation: string;
};
export type TrialView = { trial: Trial; product: Product; scans: Scan[]; verdict: Verdict };

async function j<T>(r: Promise<Response>): Promise<T> {
  const res = await r;
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body.error ?? `HTTP ${res.status}`);
  return body as T;
}

export const api = {
  catalog: () => j<Catalog>(fetch("/api/catalog")),
  units: () => j<{ units: number }>(fetch("/api/units")),
  personas: () => j<Persona[]>(fetch("/api/personas")),
  createPersona: (name: string) =>
    j<Persona>(fetch("/api/personas", { method: "POST", body: JSON.stringify({ name }) })),
  trials: () => j<Trial[]>(fetch("/api/trials")),
  createTrial: (persona_id: string, product_id: string) =>
    j<Trial>(fetch("/api/trials", { method: "POST", body: JSON.stringify({ persona_id, product_id }) })),
  trial: (id: string) => j<TrialView>(fetch(`/api/trials/${id}`)),
  scan: (id: string, image: Blob, at?: string, adherence?: string) => {
    const fd = new FormData();
    fd.append("image", image, "selfie.jpg");
    if (at) fd.append("at", at);
    if (adherence) fd.append("adherence", adherence);
    return j<Scan>(fetch(`/api/trials/${id}/scans`, { method: "POST", body: fd }));
  },
  checkin: (id: string) => j<{ trial: Trial; verdict: Verdict; action: Action }>(fetch(`/api/trials/${id}/checkin`, { method: "POST" })),
  cohorts: (byProfile = false) => j<CohortRow[]>(fetch(`/api/brand/cohorts${byProfile ? "?by=profile" : ""}`)),
  outcomes: (productId: string, profile?: string) =>
    j<{ all?: CohortRow; matching?: CohortRow }>(fetch(`/api/products/${productId}/outcomes${profile ? `?profile=${encodeURIComponent(profile)}` : ""}`)),
};

export function dataURLToBlob(dataURL: string): Blob {
  const [head, b64] = dataURL.split(",");
  const mime = /data:(.*?);/.exec(head)?.[1] ?? "image/jpeg";
  const bin = atob(b64);
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
  return new Blob([arr], { type: mime });
}
