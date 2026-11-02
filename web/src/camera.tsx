// YouCam JS Camera Kit wrapper. The kit enforces face size, pose and lighting
// before it will capture, which is exactly the quality gate a longitudinal
// scan needs. Falls back to a plain file input when the SDK isn't loaded
// (e.g. no HTTPS, or ingesting historical photos).
import { useEffect, useRef, useState } from "preact/hooks";
import { dataURLToBlob } from "./api";

declare global {
  interface Window { YMK?: any; __ymkReady?: boolean }
}

type Props = { onCapture: (img: Blob) => void; quality?: "relaxed" | "moderate" | "strict" };

export function Camera({ onCapture, quality = "moderate" }: Props) {
  const [ready, setReady] = useState(!!window.__ymkReady);
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState<string>("");
  const listeners = useRef<any[]>([]);

  useEffect(() => {
    const h = () => setReady(true);
    document.addEventListener("ymk-ready", h);
    return () => document.removeEventListener("ymk-ready", h);
  }, []);

  function start() {
    const YMK = window.YMK;
    if (!YMK) return;
    YMK.init({ faceDetectionMode: "skincare", imageFormat: "base64", language: "enu", qualityLevel: quality, videoQuality: "1080p" });
    listeners.current.push(
      YMK.addEventListener("faceQualityChanged", (s: any) => {
        setQ(`face ${s.hasFace ? "✓" : "✗"} · position ${s.position} · frontal ${s.frontal} · lighting ${s.lighting}`);
      }),
      YMK.addEventListener("faceDetectionCaptured", (r: any) => {
        const img = r.images?.[0]?.image;
        if (typeof img === "string") onCapture(dataURLToBlob(img));
        else if (img) onCapture(img as Blob);
        stop();
      }),
    );
    YMK.openCameraKit();
    setOpen(true);
  }

  function stop() {
    const YMK = window.YMK;
    listeners.current.forEach((id) => YMK?.removeEventListener(id));
    listeners.current = [];
    YMK?.close();
    setOpen(false);
    setQ("");
  }

  return (
    <div class="camera">
      <div id="YMK-module" />
      {q && <p class="muted small">{q}</p>}
      <div class="row">
        {!open && ready && <button onClick={start}>Scan with camera</button>}
        {open && <button class="ghost" onClick={stop}>Cancel</button>}
        <label class="ghost button">
          Upload photo
          <input type="file" accept="image/*" hidden onChange={(e) => {
            const f = (e.target as HTMLInputElement).files?.[0];
            if (f) onCapture(f);
          }} />
        </label>
      </div>
      {!ready && <p class="muted small">Camera Kit loading (needs HTTPS or localhost)…</p>}
    </div>
  );
}
