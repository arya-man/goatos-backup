"use client";

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  BookText,
  Calendar,
  Camera,
  ChevronLeft,
  ChevronRight,
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
import { copy, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// The New SOP builder is a dedicated full-page surface at <module SOP page>?compose=1 — the same
// route as the module page (never a nested /new page). Legacy `?new=1` deep-links resolve to it too.

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
  animal_id_scan: ScanLine,
  location_picker: MapPin,
  photo_proof: Camera,
  video_proof: Video,
};

const STATUS_TONE: Record<SopCardView["status"], string> = { active: "t-ok", draft: "t-mut", retired: "t-warn" };
function StatusTag({ view }: { view: SopCardView }) {
  const label = view.status === "active" ? `published${view.versionNumber ? ` · v${view.versionNumber}` : ""}` : view.status;
  return <span className={`tag ${STATUS_TONE[view.status]}`}>{label}</span>;
}

export interface SopLibraryProps {
  sops: SopCardView[];
  error?: { code?: string; message: string } | null;
  authRequired?: boolean;
  pageContract: AdminUiPageContract;
  /** The module SOP page path this library is mounted on (e.g. "/vaccination/sops"). */
  basePath: string;
}

// SOP Library client console. Ported from the mock SOP Library screen (header, search, domain chips,
// card grid, detail modal). "New SOP" / "Edit" navigate to the dedicated full-page builder
// (<basePath>?compose=1 [&edit=<sop_id>]). Cards render ONLY real `/admin/sops` data; facets are derived from
// real code/description/form_dsl/proof_policy. No mock inventory, no fake source rows.
export function SopLibrary({ sops, error, authRequired, pageContract, basePath }: SopLibraryProps) {
  const router = useRouter();
  const builderHref = `${basePath}?compose=1`;
  const [query, setQuery] = useState("");
  const [detail, setDetail] = useState<SopCardView | null>(null);
  const openBuilder = () => router.push(builderHref);
  const openEditor = (sopId: string) => router.push(`${builderHref}&edit=${sopId}`);
  const [requestedPage, setRequestedPage] = useState(1);
	  const pageSizeOptions = tablePageSizes(pageContract, "sop-library");
	  const [pageSize, setPageSize] = useState<number>(pageSizeOptions.includes(10) ? 10 : (pageSizeOptions[0] ?? 10));
  // The page is pre-scoped to its module's SOP codes (SOP split, maintainer decision 2026-08-18),
  // so the only client-side filter is the text search.
  const list = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return sops;
    return sops.filter((s) => `${s.name} ${s.code} ${s.domainLabel}`.toLowerCase().includes(q));
  }, [sops, query]);
  const totalPages = Math.max(1, Math.ceil(list.length / pageSize));
  const page = Math.min(requestedPage, totalPages);
  const start = list.length === 0 ? 0 : (page - 1) * pageSize + 1;
  const end = list.length === 0 ? 0 : Math.min(list.length, page * pageSize);
  const pagedList = list.slice((page - 1) * pageSize, page * pageSize);

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
	            {copy(pageContract, "crumb")} · <b>{pageContract.title}</b>
	          </div>
	          <h1>{pageContract.title}</h1>
	          <div className="sub">{pageContract.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <button
          type="button"
          className="btn p"
          onClick={openBuilder}
        >
	          <Plus className="ic" /> {copy(pageContract, "action.new_sop")}
        </button>
      </div>

      {/* Search + active SOP slice chips. Future domains must appear only when their module is explicitly opened. */}
      <div className="wftoolbar" style={{ marginBottom: 14 }}>
        <div className="tsearch" style={{ maxWidth: 300 }}>
          <Search className="ic" style={{ width: 15 }} />
          <input
	            aria-label={copy(pageContract, "filter.search_label")}
	            placeholder={copy(pageContract, "filter.search_placeholder")}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setRequestedPage(1);
            }}
          />
        </div>
      </div>

      {authRequired ? (
        <div className="alert warn" style={{ marginBottom: 14 }}>
          <Users className="ic" />
          <div>{copy(pageContract, "auth.sign_in")}</div>
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
	            <h3 style={{ margin: 0, fontSize: 16 }}>{copy(pageContract, "empty.title")}</h3>
            <p className="muted" style={{ maxWidth: 640, margin: "8px auto 0", lineHeight: 1.6, fontSize: 13 }}>
	              {copy(pageContract, "empty.body")}
            </p>
            <button
              type="button"
              className="btn p"
              style={{ marginTop: 14 }}
              onClick={openBuilder}
            >
	              <Plus className="ic" /> {copy(pageContract, "action.new_sop")}
            </button>
          </div>
        </section>
      ) : (
        <>
          <div className="grid g3" id="sopCards">
            {pagedList.map((s) => {
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
                      {s.stepCount !== null ? <span className="tag t-ok">{s.stepCount} {copy(pageContract, "label.steps")}</span> : null}
                      <StatusTag view={s} />
                    </div>
                    <div className="muted small">
                      {s.gates.length > 0 ? s.gates.slice(0, 3).join(" · ") : s.hasVersion ? copy(pageContract, "label.no_proof_gates") : copy(pageContract, "label.no_published_version")}
                    </div>
                  </div>
                </div>
              );
            })}
            {list.length === 0 ? <div className="note">{copy(pageContract, "empty.no_match")}</div> : null}
          </div>
          {list.length > 0 && totalPages > 1 ? (
            <div className="pager2" style={{ marginTop: 14, border: "1px solid var(--line2)", borderRadius: 10 }}>
              <span className="muted small">
                {start}-{end} {copy(pageContract, "label.of")} {list.length} SOPs · {copy(pageContract, "label.page")} {page} {copy(pageContract, "label.of")} {totalPages}
              </span>
              <span className="sp" style={{ flex: 1 }} />
              <span className="muted small">{copy(pageContract, "label.rows")}</span>
              <span className="chipset" style={{ gap: 4 }}>
	                {pageSizeOptions.map((size) => (
                  <button
                    key={size}
                    type="button"
                    className={`chip${pageSize === size ? " on" : ""}`}
                    style={{ padding: "5px 8px", fontSize: 11 }}
                    onClick={() => {
                      setPageSize(size);
                      setRequestedPage(1);
                    }}
                  >
                    {size}
                  </button>
                ))}
              </span>
              <button
                type="button"
                className="btn sm"
                disabled={page <= 1}
                aria-disabled={page <= 1 ? "true" : undefined}
                style={page <= 1 ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
                onClick={() => setRequestedPage((p) => Math.max(1, p - 1))}
              >
                <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.previous")}
              </button>
              <button
                type="button"
                className="btn sm"
                disabled={page >= totalPages}
                aria-disabled={page >= totalPages ? "true" : undefined}
                style={page >= totalPages ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
                onClick={() => setRequestedPage((p) => Math.min(totalPages, p + 1))}
              >
                {copy(pageContract, "action.next")} <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
              </button>
            </div>
          ) : null}
        </>
      )}

      {detail ? (
        <SopDetailModal
          view={detail}
          pageContract={pageContract}
          onClose={() => setDetail(null)}
          onEdit={() => {
            setDetail(null);
            openEditor(detail.sopId);
          }}
        />
      ) : null}
    </div>
  );
}

function SopDetailModal({ view, pageContract, onClose, onEdit }: { view: SopCardView; pageContract: AdminUiPageContract; onClose: () => void; onEdit: () => void }) {
  // Overlay close contract: Escape must close the modal, alongside the X button and backdrop click.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <>
      <div className="cfgback on" onClick={onClose} />
      <div className="cfgmodal on" style={{ width: "min(720px,96vw)" }} role="dialog" aria-modal="true" aria-label={`${copy(pageContract, "modal.detail.aria")} ${view.name}`}>
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
          <button type="button" className="x" onClick={onClose} aria-label={copy(pageContract, "modal.detail.close_label")}>
            <X className="ic" />
          </button>
        </div>

        <div className="cmb" style={{ display: "block" }}>
          <div className="metagrid">
            <div>
              <div className="k">{copy(pageContract, "label.domain")}</div>
              <div className="v">{view.domainLabel}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.trigger")}</div>
              <div className="v">{view.trigger ?? copy(pageContract, "label.placeholder")}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.code")}</div>
              <div className="v mono">{view.code}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.version_status")}</div>
              <div className="v">
                {view.versionLabel ?? copy(pageContract, "label.placeholder")} · {view.versionStatus ?? view.status}
              </div>
            </div>
            {view.gates.length > 0 ? (
              <div style={{ gridColumn: "1/3" }}>
                <div className="k">{copy(pageContract, "label.gates")}</div>
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
            {copy(pageContract, "label.steps_questions")}{" "}
            <span className="muted small">
              ({view.fields.length}) — {copy(pageContract, "label.render_action_center")}
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
                        {copy(pageContract, "label.type")}: {f.type}
                        {f.required ? ` · ${copy(pageContract, "label.required")}` : ""}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <div className="note">
              {copy(pageContract, "empty.no_published_fields")}
            </div>
          )}
        </div>

        <div className="cfgmf">
          <button type="button" className="btn" onClick={onClose}>
            {copy(pageContract, "action.close")}
          </button>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn p" onClick={onEdit}>
            <NotebookPen className="ic" /> {copy(pageContract, "action.new_sop_builder")}
          </button>
        </div>
      </div>
    </>
  );
}
