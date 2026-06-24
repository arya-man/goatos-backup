"use client";

import { useMemo, useState } from "react";
import {
  BookText,
  Calendar,
  Camera,
  Check,
  Clock,
  Hash,
  List,
  ListChecks,
  MapPin,
  NotebookPen,
  Plus,
  ScanLine,
  Search,
  SquarePen,
  Type as TypeIcon,
  Users,
  Video,
  X,
  Zap,
} from "lucide-react";
import { type SopCardView, type SopTrigger } from "./sop-derive";
import { NewSopModal } from "./new-sop-modal";

const TRIGGER_ICON: Record<SopTrigger, React.ElementType> = {
  form: SquarePen,
  cron: Clock,
  sensor: Zap,
  manual: Users,
};

const FIELD_ICON: Record<string, React.ElementType> = {
  text: TypeIcon,
  number: Hash,
  date_time: Calendar,
  select: List,
  multiselect: ListChecks,
  goat_lookup: ScanLine,
  rfid_scan: ScanLine,
  location_picker: MapPin,
  photo_proof: Camera,
  video_proof: Video,
};

const STATUS_TONE: Record<SopCardView["status"], string> = { active: "t-ok", draft: "t-mut", retired: "t-warn" };
const ACTIVE_SOP_CHIPS = [
  { id: "vaccination", label: "Vaccination" },
] as const;

function StatusTag({ view }: { view: SopCardView }) {
  const label = view.status === "active" ? `published${view.versionNumber ? ` · v${view.versionNumber}` : ""}` : view.status;
  return <span className={`tag ${STATUS_TONE[view.status]}`}>{label}</span>;
}

export interface SopLibraryProps {
  sops: SopCardView[];
  error?: { code?: string; message: string } | null;
  authRequired?: boolean;
}

// SOP Library client console. Ported from the mock SOP Library screen (header, search, domain chips,
// card grid, detail modal) + New SOP builder modal. Cards render ONLY real `/admin/sops` data; facets
// are derived from real code/description/form_dsl/proof_policy. No mock inventory, no fake source rows.
export function SopLibrary({ sops, error, authRequired }: SopLibraryProps) {
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);
  const [detail, setDetail] = useState<SopCardView | null>(null);

  // Counts are computed from the vaccination-visible slice only (sops already filtered to vaccination
  // on the server). The only live bucket is "All"; the other mock domain chips stay for layout fidelity
  // but read 0 and are disabled — they must not imply built product.
  const list = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return sops;
    return sops.filter((s) => `${s.name} ${s.code} ${s.domainLabel}`.toLowerCase().includes(q));
  }, [sops, query]);

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            Admin · Data Ops · <b>SOP Library</b>
          </div>
          <h1>SOP Library</h1>
          <div className="sub">
            <b>Current slice: PHC / Vaccination.</b> Vaccination drive/session SOP policy — proof gates, central
            verification, repeat-per-goat, and vaccine batch / cold-chain fields. Authored as versioned{" "}
            <span className="mono">form_dsl</span> + <span className="mono">proof_policy</span>; other SOP domains are
            not built yet.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <button type="button" className="btn p" onClick={() => setCreating(true)}>
          <Plus className="ic" /> New SOP
        </button>
      </div>

      {/* Search + active SOP slice chips. Future domains must appear only when their module is explicitly opened. */}
      <div className="wftoolbar" style={{ marginBottom: 14 }}>
        <div className="tsearch" style={{ maxWidth: 300 }}>
          <Search className="ic" style={{ width: 15 }} />
          <input
            aria-label="Search SOPs"
            placeholder="Search SOPs…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
        <div className="subtabs" style={{ margin: 0 }}>
          {ACTIVE_SOP_CHIPS.map((c) => {
            return (
              <button
                key={c.id}
                type="button"
                className="on"
                title="Current visible SOP slice is PHC / Vaccination"
              >
                {c.label}
                <span className="cbq">{sops.length}</span>
              </button>
            );
          })}
        </div>
      </div>

      {authRequired ? (
        <div className="alert warn" style={{ marginBottom: 14 }}>
          <Users className="ic" />
          <div>Sign in with Google to load the SOP Library — the admin SOP engine is tenant-scoped.</div>
        </div>
      ) : error ? (
        <div className="alert warn" style={{ marginBottom: 14 }}>
          <X className="ic" />
          <div>
            {error.code ? <b>{error.code}&nbsp;</b> : null}
            {error.message}
          </div>
        </div>
      ) : null}

      {sops.length === 0 && !authRequired && !error ? (
        // Mock-matching empty state — the engine stands up empty; no sample cards are fabricated.
        <section className="card">
          <div className="bd" style={{ textAlign: "center", padding: 32 }}>
            <BookText className="ic" aria-hidden="true" style={{ width: 24, height: 24, marginBottom: 10, color: "var(--brand)" }} />
            <h3 style={{ margin: 0, fontSize: 16 }}>No vaccination SOPs yet</h3>
            <p className="muted" style={{ maxWidth: 640, margin: "8px auto 0", lineHeight: 1.6, fontSize: 13 }}>
              This slice shows vaccination SOPs only (<span className="mono">vaccination.*</span>). Any non-vaccination
              SOPs the backend holds are hidden here. Author a vaccination SOP with <b>New SOP</b> — it creates an SOP
              definition and a draft version (<span className="mono">form_dsl</span> + <span className="mono">proof_policy</span>)
              via the real admin API. Cards appear here only when real vaccination SOP data exists.
            </p>
            <button type="button" className="btn p" style={{ marginTop: 14 }} onClick={() => setCreating(true)}>
              <Plus className="ic" /> New SOP
            </button>
          </div>
        </section>
      ) : (
        <div className="grid g3" id="sopCards">
          {list.map((s) => {
            const TrigIcon = s.trigger ? TRIGGER_ICON[s.trigger] : BookText;
            return (
              <div
                key={s.sopId}
                className="card"
                role="button"
                tabIndex={0}
                style={{ cursor: "pointer" }}
                onClick={() => setDetail(s)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    setDetail(s);
                  }
                }}
              >
                <div className="hd">
                  <span className="fic" style={{ width: 26, height: 26, background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                    <TrigIcon className="ic" style={{ width: 14 }} />
                  </span>
                  <h3 style={{ fontSize: 14 }}>{s.name}</h3>
                </div>
                <div className="bd">
                  <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 8 }}>
                    <span className="tag t-mut">{s.domainLabel}</span>
                    {s.trigger ? <span className="tag t-info">{s.trigger}</span> : null}
                    {s.stepCount !== null ? <span className="tag t-ok">{s.stepCount} steps</span> : null}
                    <StatusTag view={s} />
                  </div>
                  <div className="muted small">
                    {s.gates.length > 0 ? s.gates.slice(0, 3).join(" · ") : s.hasVersion ? "no proof gates" : "no published version"}
                  </div>
                </div>
              </div>
            );
          })}
          {list.length === 0 ? <div className="note">No SOPs match.</div> : null}
        </div>
      )}

      {detail ? <SopDetailModal view={detail} onClose={() => setDetail(null)} onEdit={() => { setDetail(null); setCreating(true); }} /> : null}
      <NewSopModal open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

function SopDetailModal({ view, onClose, onEdit }: { view: SopCardView; onClose: () => void; onEdit: () => void }) {
  return (
    <>
      <div className="cfgback on" onClick={onClose} />
      <div className="cfgmodal on" style={{ width: "min(720px,96vw)" }} role="dialog" aria-modal="true" aria-label={`SOP ${view.name}`}>
        <div className="cmh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}>
            <BookText className="ic" />
          </span>
          <div>
            <div className="mono muted" style={{ fontSize: 11 }}>
              SOP · {view.domainLabel.toUpperCase()}
            </div>
            <div className="b700">{view.name}</div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="x" onClick={onClose} aria-label="Close">
            <X className="ic" />
          </button>
        </div>

        <div className="cmb" style={{ display: "block" }}>
          <div className="metagrid">
            <div>
              <div className="k">Domain</div>
              <div className="v">{view.domainLabel}</div>
            </div>
            <div>
              <div className="k">Trigger</div>
              <div className="v">{view.trigger ?? "—"}</div>
            </div>
            <div>
              <div className="k">Code</div>
              <div className="v mono">{view.code}</div>
            </div>
            <div>
              <div className="k">Version · status</div>
              <div className="v">
                {view.versionLabel ?? "—"} · {view.versionStatus ?? view.status}
              </div>
            </div>
            {view.gates.length > 0 ? (
              <div style={{ gridColumn: "1/3" }}>
                <div className="k">Gates</div>
                <div className="v" style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                  {view.gates.map((g) => (
                    <span key={g} className="tag t-pur">
                      {g}
                    </span>
                  ))}
                </div>
              </div>
            ) : null}
          </div>

          {view.description ? (
            <div className="muted small" style={{ margin: "10px 0" }}>
              {view.description}
            </div>
          ) : null}

          <div className="b700" style={{ margin: "8px 0" }}>
            Steps &amp; questions{" "}
            <span className="muted small">
              ({view.fields.length}) — render into Action Center tasks
            </span>
          </div>
          {view.fields.length > 0 ? (
            <div className="htl">
              {view.fields.map((f, i) => {
                const Icon = FIELD_ICON[f.type] ?? Check;
                return (
                  <div className="hrow" key={`${f.label}-${i}`}>
                    <span className="fic" style={{ width: 24, height: 24, background: "var(--bg)", color: "var(--muted)" }}>
                      <Icon className="ic" style={{ width: 13 }} />
                    </span>
                    <div className="htx">
                      <b>
                        {i + 1}. {f.label}
                      </b>
                      <div className="hmeta muted small">
                        type: {f.type}
                        {f.required ? " · required" : ""}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <div className="note">
              No published version yet — this SOP has no <span className="mono">form_dsl</span> fields to show. Open the
              builder to author a draft version.
            </div>
          )}
        </div>

        <div className="cfgmf">
          <button type="button" className="btn" onClick={onClose}>
            Close
          </button>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn p" onClick={onEdit}>
            <NotebookPen className="ic" /> New SOP in builder
          </button>
        </div>
      </div>
    </>
  );
}
