// YouCam JS Camera Kit wrapper. The kit enforces face size, pose and lighting
// before it will capture, which is exactly the quality gate a longitudinal
// scan needs. Falls back to a plain file input when the SDK isn't loaded
// (e.g. no HTTPS, or ingesting historical photos).
import { useEffect, useRef, useState } from "preact/hooks";
import { dataURLToBlob } from "./api";
import { Icon } from "./icons";

declare global {
  interface Window { YMK?: any; __ymkReady?: boolean }
}

type Props = { onCapture: (img: Blob) => void; disabled?: boolean };
type Q = Record<"face" | "dist" | "angle" | "light", string>;
const WAIT: Q = { face: "wait", dist: "wait", angle: "wait", light: "wait" };

function mapQuality(s: any) {
  const dist = s.area ?? s.position; // SDK v2.5 sends `area`; older builds `position`
  const st = (ok: boolean | undefined) => (ok === undefined ? "wait" : ok ? "good" : "bad");
  const q: Q = { face: st(s.hasFace), dist: st(dist === undefined ? undefined : dist === "good"),
              angle: st(s.frontal === undefined ? undefined : s.frontal === "good"),
              light: st(s.lighting === undefined ? undefined : s.lighting === "good" || s.lighting === "ok") };
  const tip = q.face === "bad" ? "Put your face in the frame"
    : q.dist === "bad" ? ({ toosmall: "Move closer", toolarge: "Move back a little", outofboundary: "Centre your face" } as any)[dist] ?? "Adjust your distance"
    : q.angle === "bad" ? "Look straight at the camera"
    : q.light === "bad" ? "Find softer, brighter light"
    : Object.values(q).every((v) => v === "good") ? "Hold still, capturing…" : "Checking your frame…";
  return { q, tip };
}

const CHIPS: [keyof Q, string, string][] = [["face", "Face", "face"], ["dist", "Distance", "distance"], ["angle", "Angle", "angle"], ["light", "Light", "sun"]];

export function Camera({ onCapture, disabled }: Props) {
  const [ready, setReady] = useState(!!window.__ymkReady);
  const [unavailable, setUnavailable] = useState(false);
  const [failed, setFailed] = useState("");
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState<Q>(WAIT);
  const [tip, setTip] = useState("");
  const listeners = useRef<any[]>([]);

  useEffect(() => {
    const timer = setTimeout(() => { if (!window.__ymkReady) setUnavailable(true); }, 5000);
    const h = () => { clearTimeout(timer); setUnavailable(false); setReady(true); };
    if (window.__ymkReady) clearTimeout(timer);
    document.addEventListener("ymk-ready", h);
    return () => { clearTimeout(timer); document.removeEventListener("ymk-ready", h); cleanup(); };
  }, []);

  function start() {
    const YMK = window.YMK;
    if (!YMK) return;
    setFailed("");
    YMK.init({ faceDetectionMode: "skincare", imageFormat: "base64", language: "enu", qualityLevel: "moderate", videoQuality: "1080p" });
    listeners.current.push(
      YMK.addEventListener("faceQualityChanged", (s: any) => { const m = mapQuality(s); setQ(m.q); setTip(m.tip); }),
      YMK.addEventListener("faceDetectionCaptured", (r: any) => {
        const img = r.images?.[0]?.image;
        if (typeof img === "string") onCapture(dataURLToBlob(img));
        else if (img) onCapture(img as Blob);
        stop();
      }),
      YMK.addEventListener("cameraFailed", () => { setFailed("Allow camera access, or upload a photo."); stop(); }),
      YMK.addEventListener("closed", () => cleanup()), // kit's own close button: already closed
    );
    YMK.openCameraKit();
    setOpen(true);
    document.documentElement.style.overflow = "hidden";
  }

  function cleanup() {
    listeners.current.forEach((id) => window.YMK?.removeEventListener(id));
    listeners.current = [];
    setOpen(false);
    setQ(WAIT);
    setTip("");
    document.documentElement.style.overflow = "";
  }

  function stop() {
    window.YMK?.close();
    cleanup();
  }

  return (
    <div class="camera" style="display:grid;gap:10px">
      <div id="YMK-module" class={open ? "full" : ""} />
      {open && <div class="cam-bar">
        <p class="cam-tip" aria-live="polite">{tip || "Checking your frame…"}</p>
        <div class="qchips">{CHIPS.map(([k, l, i]) =>
          <div class={"qchip " + q[k]}><Icon n={i} /><span>{l}</span><b>{q[k] === "good" ? "OK" : q[k] === "bad" ? "Fix" : "Checking"}</b></div>)}</div>
        <button class="block" onClick={stop}>Cancel</button>
      </div>}
      {!open && <>
        <p class="t-sub">We check light and angle so week 4 compares fairly to day 0. Your camera opens full screen.</p>
        {ready && !failed && <button class="block" onClick={start} disabled={disabled}><Icon n="camera" />Open camera</button>}
        {!ready && !unavailable && <p class="note" role="status"><span class="spin" aria-hidden="true" />Starting YouCam Camera Kit…</p>}
        {(unavailable || failed) && <p class="alert" role="status"><Icon n="alert" />{failed || "Camera unavailable here. Upload a photo."}</p>}
        <label class="btn tinted block" tabIndex={0} onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); (e.currentTarget.querySelector("input") as HTMLInputElement).click(); } }}>
          <Icon n="upload" />Upload photo · not quality-checked
          <input type="file" accept="image/*" hidden onChange={(e) => {
            const f = (e.target as HTMLInputElement).files?.[0];
            if (f) onCapture(f);
          }} />
        </label>
      </>}
    </div>
  );
}
