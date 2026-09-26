import type { ReactElement } from "react";

// Scoped chrome for the admin-only step-trace viewer. Every colour is a shared
// mesha theme token (--paper, --fg, --line-strong, --error-soft, ...), so the
// surface renders correctly in BOTH the dark and light themes; the previous
// hardcoded Tailwind slate/rose classes were dark-only and off-brand. Prefix
// `mzat-` keeps it isolated from the assistant chat (`mzai-`) and the kit.
export function TraceViewerStyles(): ReactElement {
  return (
    <style>{`
.mzat-form{display:flex;flex-wrap:wrap;align-items:flex-end;gap:16px}
.mzat-field{display:flex;flex-direction:column;gap:6px;min-width:0;flex:1 1 380px}
.mzat-field span{font-size:12px;font-weight:600;letter-spacing:.02em;color:var(--fg-muted)}
.mzat-input{width:100%;height:40px;padding:0 12px;font:inherit;font-size:14px;border-radius:var(--r-md);
  border:1px solid var(--line-strong);background:var(--paper-2);color:var(--fg);outline:0;
  transition:border-color var(--dur-fast) var(--ease),box-shadow var(--dur-fast) var(--ease)}
.mzat-input::placeholder{color:var(--fg-faint)}
.mzat-input:focus{border-color:var(--primary);box-shadow:0 0 0 3px var(--ring)}
.mzat-alert{display:flex;align-items:flex-start;gap:10px;padding:14px 16px;border-radius:var(--r-lg);
  font-size:14px;line-height:1.5;background:var(--error-soft);color:var(--error-ink);
  border:1px solid color-mix(in srgb,var(--error) 38%,transparent)}
.mzat-alert svg{width:18px;height:18px;flex:none;margin-top:1px}
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
.mzat-stack{display:flex;flex-direction:column;gap:24px}
@media (max-width:640px){
  .mzat-form{gap:12px}
  .mzat-field{flex:1 1 100%}
  .mzat-metas{grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:12px}
}
@media (prefers-reduced-motion: reduce){
  .mzat-meta,.mzat-table tbody tr,.mzat-pill,.mzat-input{transition:none}
  .mzat-meta:hover{transform:none}
}
`}</style>
  );
}
