const P: Record<string, string> = {
  checkCircle: "M21 12a9 9 0 1 1-18 0a9 9 0 0 1 18 0M8 12.5l3 3 5-6",
  xCircle: "M21 12a9 9 0 1 1-18 0a9 9 0 0 1 18 0M9 9l6 6M15 9l-6 6",
  clock: "M21 12a9 9 0 1 1-18 0a9 9 0 0 1 18 0M12 7v5l3 2",
  check: "M5 12.5l4.5 4.5L19 7.5",
  repeat: "M4 12a8 8 0 0 1 13.7-5.6L20 9M20 4v5h-5M20 12a8 8 0 0 1-13.7 5.6L4 15M4 20v-5h5",
  swap: "M7 7h13l-4-4M17 17H4l4 4",
  tag: "M3 12V4h8l10 10-8 8zM7.5 7.5h.01",
  play: "M8 5v14l11-7z",
  chevR: "M9 6l6 6-6 6", chevL: "M15 6l-6 6 6 6",
  plus: "M12 5v14M5 12h14",
  camera: "M4 8h3l2-3h6l2 3h3v11H4zM12 17a3.5 3.5 0 1 0 0-7a3.5 3.5 0 0 0 0 7",
  upload: "M12 16V4M7 9l5-5 5 5M4 20h16",
  list: "M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01",
  chart: "M5 20V11M12 20V5M19 20v-6",
  alert: "M12 3l10 18H2zM12 10v5M12 18h.01",
  face: "M21 12a9 9 0 1 1-18 0a9 9 0 0 1 18 0M9 10h.01M15 10h.01M9 15c1.7 1.3 4.3 1.3 6 0",
  distance: "M4 12h16M8 8l-4 4 4 4M16 8l4 4-4 4",
  angle: "M12 3a9 9 0 1 0 0 18M12 3c-3 3-3 15 0 18",
  sun: "M12 16a4 4 0 1 0 0-8a4 4 0 0 0 0 8M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4",
};
export const Icon = ({ n, cls = "" }: { n: string; cls?: string }) =>
  <svg class={"ico " + cls} viewBox="0 0 24 24" aria-hidden="true"><path d={P[n]} /></svg>;
