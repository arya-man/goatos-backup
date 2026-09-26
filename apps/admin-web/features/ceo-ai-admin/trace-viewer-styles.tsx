import type { ReactElement } from "react";

// Scoped chrome for the admin-only step-trace viewer. Every colour is a shared
// mesha theme token (--paper, --fg, --line-strong, --error-soft, ...), so the
// surface renders correctly in BOTH the dark and light themes; the previous
// hardcoded Tailwind slate/rose classes were dark-only and off-brand. Prefix
// `mzat-` keeps it isolated from the assistant chat (`mzai-`) and the kit.
export function TraceViewerStyles(): ReactElement {
  return (
    <style>{`
.mzat-metas{display:grid;gap:16px;grid-template-columns:repeat(auto-fit,minmax(180px,1fr))}
.mzat-meta{display:flex;flex-direction:column;gap:6px;padding:16px;border-radius:var(--r-lg);
  background:var(--paper-2);transition:transform 220ms var(--ease),box-shadow 300ms var(--ease)}
.mzat-meta:hover{transform:translateY(-2px);box-shadow:var(--shadow-card)}
.mzat-meta-k{font-size:11px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:var(--fg-muted)}
.mzat-meta-v{font-size:15px;font-weight:600;color:var(--fg);font-variant-numeric:tabular-nums;
  overflow-wrap:anywhere}
.mzat-chip{display:inline-flex;align-items:center;gap:6px;height:24px;padding:0 10px;border-radius:var(--r-pill);
  font-size:12px;font-weight:700;letter-spacing:.01em}
.mzat-body{white-space:pre-wrap;overflow-wrap:anywhere;font-size:14px;line-height:1.65;color:var(--fg);margin:0}
.mzat-pills{display:flex;flex-wrap:wrap;gap:8px}
.mzat-pill{display:inline-flex;align-items:center;height:26px;padding:0 10px;border-radius:var(--r-sm);
  font-size:12px;font-weight:600;background:var(--paper-2);color:var(--fg-muted);
  transition:color var(--dur-fast) var(--ease),background-color var(--dur-fast) var(--ease)}
.mzat-pill:hover{background:var(--hover);color:var(--fg)}
.mzat-tablewrap{overflow-x:auto;border-radius:var(--r-lg)}
.mzat-table{width:100%;border-collapse:separate;border-spacing:0;font-size:13.5px}
.mzat-table th{position:sticky;top:0;z-index:1;text-align:left;white-space:nowrap;padding:12px 14px;
  font-weight:600;font-size:13px;color:var(--fg-muted);background:var(--paper-2)}
.mzat-table th:first-child{border-top-left-radius:var(--r-md);border-bottom-left-radius:var(--r-md)}
.mzat-table th:last-child{border-top-right-radius:var(--r-md);border-bottom-right-radius:var(--r-md)}
.mzat-table td{padding:12px 14px;border-bottom:1px solid var(--line);vertical-align:top;color:var(--fg)}
.mzat-table tbody tr{transition:background-color var(--dur-fast) var(--ease)}
.mzat-table tbody tr:hover>td{background:var(--hover)}
.mzat-table tbody tr:last-child td{border-bottom:0}
.mzat-num{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap;color:var(--fg-muted)}
.mzat-idx{display:inline-grid;place-items:center;min-width:24px;height:24px;padding:0 6px;border-radius:var(--r-sm);
  background:var(--primary-soft);color:var(--primary-ink);font-size:12px;font-weight:700}
.mzat-q{color:var(--fg);overflow-wrap:anywhere}
.mzat-params{margin:8px 0 0;padding:10px 12px;border-radius:var(--r-md);background:var(--bg-subtle);
  color:var(--fg-muted);font-size:12px;line-height:1.5;overflow-x:auto}
.mzat-note{margin:6px 0 0;font-size:12px;color:var(--fg-muted)}
.mzat-note.err{color:var(--error-ink)}
.mzat-empty{padding:28px 8px;text-align:center;font-size:14px;color:var(--fg-muted)}
.mzat-stamp{font-size:12px;color:var(--fg-faint)}
@media (max-width:640px){
  .mzat-metas{grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:12px}
}
@media (prefers-reduced-motion: reduce){
  .mzat-meta,.mzat-table tbody tr,.mzat-pill{transition:none}
  .mzat-meta:hover{transform:none}
}
`}</style>
  );
}
