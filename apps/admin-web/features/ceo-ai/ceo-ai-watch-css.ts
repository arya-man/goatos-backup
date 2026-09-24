// Styles for the Ask Mesha live ear-tag watch card. Directive-free so the server-rendered
// ceo-ai-styles.tsx can inline it (a "use client" module export would be a client reference).

export const WATCH_CSS = `
.mzai-watch{border:1px solid var(--line);border-radius:12px;background:var(--panel);margin:4px 0 10px;overflow:hidden;max-width:100%}
.mzai-watch.live{border-color:color-mix(in srgb,var(--brand) 45%,var(--line))}
.mzai-w-head{display:flex;align-items:center;gap:10px;flex-wrap:wrap;padding:10px 12px;border-bottom:1px solid var(--line);background:var(--panel-2)}
.mzai-w-dot{width:8px;height:8px;border-radius:50%;background:var(--muted);flex:none}
.mzai-w-dot.live{background:var(--brand);box-shadow:0 0 0 0 color-mix(in srgb,var(--brand) 60%,transparent);animation:mzai-w-pulse 1.6s ease-out infinite}
@keyframes mzai-w-pulse{to{box-shadow:0 0 0 8px transparent}}
.mzai-w-title{display:flex;flex-direction:column;min-width:0;flex:1 1 160px}
.mzai-w-title strong{font-size:13px;color:var(--ink)}
.mzai-w-sub{font-size:11.5px;color:var(--muted);overflow-wrap:anywhere}
.mzai-w-count{font-size:12px;color:var(--muted);font-variant-numeric:tabular-nums}
.mzai-w-stop{border:1.5px solid var(--brand);background:var(--panel);color:var(--ink);border-radius:999px;padding:5px 12px;font:inherit;font-size:12px;font-weight:600;cursor:pointer;min-height:32px}
.mzai-w-stop:hover{background:var(--brand-soft)}
.mzai-w-err,.mzai-w-quiet{margin:8px 12px;font-size:12px;color:var(--muted)}
.mzai-w-scroll{overflow-x:auto;-webkit-overflow-scrolling:touch;max-height:320px;overflow-y:auto}
.mzai-w-table{border-collapse:collapse;width:100%;font-size:12px;color:var(--ink)}
.mzai-w-table th{position:sticky;top:0;background:var(--panel);text-align:left;font-weight:600;color:var(--muted);padding:6px 10px;border-bottom:1px solid var(--line);white-space:nowrap}
.mzai-w-table td{padding:6px 10px;border-bottom:1px solid var(--line);vertical-align:top;white-space:nowrap}
.mzai-w-table .num{text-align:right;font-variant-numeric:tabular-nums}
.mzai-w-tag{font-weight:600}
.mzai-w-dim{display:block;font-size:11px;color:var(--muted)}
.mzai-w-pill{display:inline-block;border-radius:999px;padding:1px 8px;font-size:11px;font-weight:600;background:color-mix(in srgb,var(--muted) 14%,transparent);color:var(--ink)}
.mzai-w-pill.ok{background:var(--okx)}
.mzai-w-pill.teal{background:var(--tealx)}
.mzai-w-pill.warn{background:var(--warnx)}
.mzai-w-pill.dng{background:var(--dangerx)}
.mzai-w-pill.pur{background:var(--purplex)}
.mzai-w-pct{font-weight:600}
.mzai-w-pct.dng{color:var(--danger)}
.mzai-w-pct.warn{color:var(--warn)}
.mzai-w-pct.mut{color:var(--muted)}
.mzai-w-feed{list-style:none;margin:0;padding:8px 12px;display:flex;flex-direction:column;gap:4px;max-height:160px;overflow-y:auto;font-size:12px;color:var(--ink)}
.mzai-w-feed li{display:flex;gap:8px;align-items:baseline;border-left:3px solid var(--line);padding-left:8px}
.mzai-w-feed li.ok{border-left-color:var(--brand)}
.mzai-w-feed li.warn{border-left-color:var(--warn)}
.mzai-w-feed li.dng{border-left-color:var(--danger)}
.mzai-w-at{flex:none;min-width:34px;color:var(--muted);font-variant-numeric:tabular-nums}
@media (max-width:620px){.mzai-w-table td,.mzai-w-table th{padding:6px 8px}.mzai-w-head{padding:10px}}
@media (prefers-reduced-motion:reduce){.mzai-w-dot.live{animation:none}}
`;
