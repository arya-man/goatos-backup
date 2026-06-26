"use client";

// Counts -> Herd Register write UI: the "Register goat" drawer (single create) and the "Import sheet" drawer
// (bulk preview -> commit). Mock-faithful drawer behavior (right panel, backdrop/Escape close, focus, footer
// actions) over the Mesha theme. Single create posts a real server action (createGoatAction) and redirects
// with a banner; bulk calls real preview/commit server actions and holds ONLY the backend's response as
// transient UI state. No fixtures, no fake totals, no client-only business mutation.
import { useEffect, useRef, useState, useTransition } from "react";
import { useFormStatus } from "react-dom";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, Download, Plus, Upload, X } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import type { LocationOption } from "@/lib/api/herd-locations";
import type { AdminGoatBulkResponse, AdminGoatBulkRowResult, CreateAdminGoatRequest } from "@/lib/api/server";
import { commitGoatsAction, createGoatAction, previewGoatsAction } from "./herd-actions";

const BULK_COLUMNS = [
  "Farm", "RFID", "Old tag", "Park", "Shed", "Breed", "Sex", "DOB", "Weight(kg)", "Dam ID", "Sire/lot", "Origin", "Photo URL",
] as const;

const SEX_OPTIONS: ReadonlyArray<{ value: string; label: string }> = [
  { value: "female", label: "Female" },
  { value: "male", label: "Male" },
  { value: "unknown", label: "Unknown" },
];

const ORIGIN_OPTIONS: ReadonlyArray<{ value: string; label: string }> = [
  { value: "birth", label: "Farm-born (birth)" },
  { value: "procured", label: "Procured" },
  { value: "imported", label: "Imported" },
  { value: "unknown", label: "Unknown" },
];

function todayISO(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
}

// Stable, path-independent content key over the CSV. Sent on preview AND commit so the backend's
// file-hash + row-fingerprint idempotency dedupes a re-submitted file. Opaque to the backend — a fast
// non-cryptographic hash is sufficient as a content key.
function fnv1aHex(input: string): string {
  let h = 0x811c9dc5;
  for (let i = 0; i < input.length; i += 1) {
    h ^= input.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0).toString(16).padStart(8, "0");
}

function decisionTone(decision: AdminGoatBulkRowResult["decision"]): "ok" | "warn" | "dng" | "mut" {
  switch (decision) {
    case "create":
      return "ok";
    case "requires_review":
      return "warn";
    case "conflict":
      return "dng";
    default:
      return "mut";
  }
}

// ---- Drawer shell (right panel; backdrop + Escape close; focus trap entry; body scroll lock) ----
function Drawer({
  open,
  onClose,
  title,
  subtitle,
  width = 560,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  subtitle?: string;
  width?: number;
  children: React.ReactNode;
}) {
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const node = panelRef.current;
    const first = node?.querySelector<HTMLElement>(
      'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled])',
    );
    (first ?? node)?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      role="presentation"
      onClick={onClose}
      style={{ position: "fixed", inset: 0, zIndex: 220, background: "rgba(0,0,0,.55)", display: "flex", justifyContent: "flex-end" }}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onClick={(event) => event.stopPropagation()}
        style={{
          width: `min(${width}px, 100%)`,
          height: "100%",
          background: "var(--panel)",
          borderLeft: "1px solid var(--line)",
          boxShadow: "var(--shadow)",
          display: "flex",
          flexDirection: "column",
          outline: "none",
        }}
      >
        <div className="hd" style={{ flexShrink: 0 }}>
          <div>
            <h3>{title}</h3>
            {subtitle ? <div className="muted small" style={{ marginTop: 2 }}>{subtitle}</div> : null}
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn sm" onClick={onClose} aria-label="Close">
            <X className="ic" style={{ width: 14 }} aria-hidden="true" />
          </button>
        </div>
        <div style={{ flex: 1, overflowY: "auto", padding: "14px 16px" }}>{children}</div>
      </div>
    </div>
  );
}

function SubmitButton({ children }: { children: React.ReactNode }) {
  const { pending } = useFormStatus();
  return (
    <button type="submit" className="btn p" disabled={pending} aria-busy={pending}>
      {pending ? "Working…" : children}
    </button>
  );
}

function Row({ children }: { children: React.ReactNode }) {
  return <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>{children}</div>;
}

// ---- Register goat drawer (single create) ----
function RegisterGoatDrawer({
  open,
  onClose,
  parks,
  sheds,
  farms,
  locationsAvailable,
  idempotencyKey,
  returnTo,
}: {
  open: boolean;
  onClose: () => void;
  parks: LocationOption[];
  sheds: LocationOption[];
  farms: LocationOption[];
  locationsAvailable: boolean;
  idempotencyKey: string;
  returnTo: string;
}) {
  const [parkId, setParkId] = useState<string>(parks[0]?.id ?? "");
  const scopedSheds = sheds.filter((s) => s.parentId === parkId);
  const shedOptions = scopedSheds.length > 0 ? scopedSheds : sheds;

  const canCreate = locationsAvailable && parks.length > 0 && shedOptions.length > 0;

  return (
    <Drawer open={open} onClose={onClose} title="Register goat" subtitle="Creates one canonical goat → emits goat.created → vaccination obligations generate.">
      {!canCreate ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>Locations unavailable</b>
            <div className="small">A clean create needs a real park and vaccination-usable shed from the locations master. Configure locations (or check the backend) before registering — no goat is created without a valid park/shed.</div>
          </div>
        </div>
      ) : null}

      {/* The action redirects (banner). Close the drawer as the form submits so the banner is visible and
          the client drawer state does not linger over the navigated page. onSubmit fires only after the
          browser's required-field validation passes, and React still dispatches the action this event. */}
      <form action={createGoatAction} onSubmit={() => onClose()} className="fld" style={{ margin: 0 }}>
        <input type="hidden" name="idempotency_key" value={idempotencyKey} />
        <input type="hidden" name="return_to" value={returnTo} />
        <input type="hidden" name="evidence_type" value="source_record" />

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label htmlFor="rg_rfid">RFID</label>
            <input id="rg_rfid" name="rfid" placeholder="e.g. RF-9001" />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label htmlFor="rg_oldtag">Old / ear tag</label>
            <input id="rg_oldtag" name="old_tag" placeholder="e.g. CB-201" />
          </div>
        </Row>
        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label htmlFor="rg_temp">Temporary field id</label>
            <input id="rg_temp" name="temp_field_id" placeholder="for untagged kids (2 IDs due 24h)" />
          </div>
        </Row>
        <div className="note" style={{ marginBottom: 12 }}>At least one identifier is required — RFID, old tag, or a temporary field id. A duplicate active RFID returns a conflict for review, not a silent merge.</div>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_park">Park (required)</label>
            <select id="rg_park" name="park_id" value={parkId} onChange={(e) => setParkId(e.target.value)} required disabled={!canCreate}>
              {parks.length === 0 ? <option value="">No parks available</option> : null}
              {parks.map((p) => (
                <option key={p.id} value={p.id}>{p.name}{p.code ? ` · ${p.code}` : ""}</option>
              ))}
            </select>
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_shed">Shed (required)</label>
            <select id="rg_shed" name="shed_id" required disabled={!canCreate} defaultValue={shedOptions[0]?.id ?? ""}>
              {shedOptions.length === 0 ? <option value="">No vaccination-usable sheds</option> : null}
              {shedOptions.map((s) => (
                <option key={s.id} value={s.id}>{s.name}{s.code ? ` · ${s.code}` : ""}</option>
              ))}
            </select>
          </div>
        </Row>
        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_farm">Farm</label>
            <select id="rg_farm" name="farm_id" defaultValue="">
              <option value="">— optional —</option>
              {farms.map((f) => (
                <option key={f.id} value={f.id}>{f.name}{f.code ? ` · ${f.code}` : ""}</option>
              ))}
            </select>
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_breed">Breed</label>
            <input id="rg_breed" name="breed" placeholder="e.g. Beetal" />
          </div>
        </Row>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 140 }}>
            <label htmlFor="rg_sex">Sex</label>
            <select id="rg_sex" name="sex" defaultValue="female">
              {SEX_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_origin">Origin</label>
            <select id="rg_origin" name="origin_type" defaultValue="birth">
              {ORIGIN_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>
        </Row>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 150 }}>
            <label htmlFor="rg_dob">DOB</label>
            <input id="rg_dob" name="dob" type="date" />
          </div>
          <div className="fld" style={{ width: 140 }}>
            <label htmlFor="rg_weight">Weight (kg)</label>
            <input id="rg_weight" name="weight_kg" type="number" min={0} step="0.1" placeholder="e.g. 22.0" />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 150 }}>
            <label htmlFor="rg_entry">Entry date (required)</label>
            <input id="rg_entry" name="entry_date" type="date" defaultValue={todayISO()} required />
          </div>
        </Row>
        <label style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 10, fontSize: 12.5 }}>
          <input name="dob_estimated" type="checkbox" style={{ width: "auto" }} /> DOB is estimated
        </label>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_dam">Dam (mother) id — lineage</label>
            <input id="rg_dam" name="dam_id" placeholder="e.g. CBE-1043" />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_sire">Sire / semen lot</label>
            <input id="rg_sire" name="sire_or_lot" placeholder="e.g. BUCK-07" />
          </div>
        </Row>

        <div className="fld">
          <label htmlFor="rg_evidence">Source / evidence reference</label>
          <input id="rg_evidence" name="evidence_id" placeholder="source doc / sheet row id (defaults to this registration's reference)" />
        </div>
        <div className="note" style={{ marginBottom: 12 }}>
          Media capture is not in this slice. Provenance is recorded as a source-record evidence ref; photo upload happens via the field app / proof API.
        </div>

        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", paddingTop: 4 }}>
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          {canCreate ? (
            <SubmitButton>Register goat</SubmitButton>
          ) : (
            <button type="button" className="btn p" disabled aria-disabled="true" style={{ opacity: 0.5, cursor: "not-allowed" }}>Register goat</button>
          )}
        </div>
      </form>
    </Drawer>
  );
}

// ---- Bulk import drawer (download template -> paste/upload CSV -> preview -> commit) ----
function BulkImportDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const router = useRouter();
  const [csv, setCsv] = useState("");
  const [preview, setPreview] = useState<AdminGoatBulkResponse | null>(null);
  const [committed, setCommitted] = useState<AdminGoatBulkResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function reset() {
    setCsv("");
    setPreview(null);
    setCommitted(null);
    setError(null);
  }

  function close() {
    reset();
    onClose();
  }

  function downloadTemplate() {
    const header = BULK_COLUMNS.join(",");
    const blob = new Blob([`${header}\n`], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "herd-register-template.csv";
    a.click();
    URL.revokeObjectURL(url);
  }

  async function onFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) return;
    setCsv(await file.text());
  }

  function runPreview() {
    setError(null);
    setCommitted(null);
    const hash = fnv1aHex(csv);
    startTransition(async () => {
      const res = await previewGoatsAction(csv, hash);
      if (res.ok) setPreview(res.data);
      else setError(`${res.error.code ?? res.error.kind}: ${res.error.message}`);
    });
  }

  function runCommit() {
    if (!preview) return;
    const rows = preview.rows
      .filter((r) => r.decision === "create" && r.normalized)
      .map((r) => r.normalized as CreateAdminGoatRequest);
    if (rows.length === 0) return;
    setError(null);
    const hash = fnv1aHex(csv);
    startTransition(async () => {
      const res = await commitGoatsAction(rows, hash);
      if (res.ok) {
        setCommitted(res.data);
        router.refresh();
      } else {
        setError(`${res.error.code ?? res.error.kind}: ${res.error.message}`);
      }
    });
  }

  const view = committed ?? preview;
  // Count what we will actually send (create rows that carry a normalized payload), so the button number
  // never overstates the commit versus summary.create_ready.
  const committable = preview ? preview.rows.filter((r) => r.decision === "create" && r.normalized).length : 0;

  return (
    <Drawer open={open} onClose={close} title="Import sheet" subtitle="Bulk register goats — download template, paste/upload CSV, preview, then commit." width={820}>
      {/* Step 1 — template + input. Hidden once a commit result is shown. */}
      {!committed ? (
        <>
          <div className="fld">
            <label>1 · Download template <span className="muted small">({BULK_COLUMNS.length} columns)</span></label>
            <button type="button" className="btn sm" onClick={downloadTemplate}>
              <Download className="ic" style={{ width: 13 }} aria-hidden="true" /> herd-register-template.csv
            </button>
            <div className="muted small" style={{ marginTop: 6 }}>{BULK_COLUMNS.join(" · ")}</div>
            <div className="note" style={{ marginTop: 8 }}>
              Each row needs at least one identifier (RFID or Old tag) plus Park and Shed (codes). Sex = female / male / unknown. Origin = birth / procured / imported / unknown. Bad rows return per-row errors below; they are never silently dropped.
            </div>
          </div>
          <div className="fld">
            <label htmlFor="bulk_file">2 · Upload CSV</label>
            <input id="bulk_file" type="file" accept=".csv,text/csv" onChange={onFile} />
          </div>
          <div className="fld">
            <label htmlFor="bulk_csv">…or paste CSV</label>
            <textarea
              id="bulk_csv"
              rows={5}
              value={csv}
              onChange={(e) => setCsv(e.target.value)}
              placeholder={BULK_COLUMNS.join(",")}
              style={{ fontFamily: "var(--mono, monospace)", fontSize: 12 }}
            />
          </div>
          <div style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 14 }}>
            <button type="button" className="btn p" onClick={runPreview} disabled={pending || csv.trim() === ""} aria-busy={pending}>
              {pending && !committed ? "Previewing…" : "Preview rows"}
            </button>
            {preview ? <span className="muted small">Previewed — review decisions below, then commit.</span> : null}
          </div>
        </>
      ) : null}

      {error ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div><b>{committed ? "Commit failed" : "Preview failed"}</b><div className="small">{error}</div></div>
        </div>
      ) : null}

      {committed ? (
        <div className="note" style={{ marginBottom: 14 }}>
          <Tag tone="ok">committed</Tag> Created {committed.summary.created} · failed {committed.summary.failed} · skipped {committed.summary.skipped} of {committed.summary.total} rows. The herd table has been refreshed.
        </div>
      ) : null}

      {/* Step 3 — preview/commit results table. */}
      {view ? (
        <>
          <div className="chipset" style={{ marginBottom: 10 }}>
            <Tag tone="mut">{view.summary.total} rows</Tag>
            <Tag tone="ok">{view.summary.create_ready} create-ready</Tag>
            <Tag tone="warn">{view.summary.requires_review} review</Tag>
            <Tag tone="mut">{view.summary.skipped} skip</Tag>
            {committed ? <Tag tone={view.summary.failed ? "dng" : "ok"}>{view.summary.created} created</Tag> : null}
          </div>
          <div className="card" style={{ marginBottom: 14 }}>
            <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
              <table>
                <thead>
                  <tr>
                    <th>Row</th>
                    <th>Decision</th>
                    <th>Identity</th>
                    <th>Notes</th>
                    {committed ? <th>Result</th> : null}
                  </tr>
                </thead>
                <tbody>
                  {view.rows.map((r) => {
                    const ident = r.normalized?.rfid || r.normalized?.old_tag || r.normalized?.temp_field_id || "—";
                    const notes = [
                      ...r.errors.map((e) => `${e.field}: ${e.message}`),
                      ...r.warnings.map((w) => w.message),
                    ].join(" · ");
                    return (
                      <tr key={r.row_number}>
                        <td className="muted">{r.row_number}</td>
                        <td><Tag tone={decisionTone(r.decision)}>{r.decision.replace(/_/g, " ")}</Tag></td>
                        <td>{ident}</td>
                        <td className="muted small">{notes || "—"}</td>
                        {committed ? (
                          <td className="muted small">
                            {r.result ? r.result.goat.display_id : r.errors.length ? "failed" : "—"}
                          </td>
                        ) : null}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>
          <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
            {committed ? (
              <button type="button" className="btn p" onClick={close}>Done</button>
            ) : (
              <>
                <button type="button" className="btn" onClick={() => { setPreview(null); setError(null); }}>Re-edit</button>
                <button
                  type="button"
                  className="btn p"
                  onClick={runCommit}
                  disabled={pending || committable === 0}
                  aria-disabled={committable === 0}
                  aria-busy={pending}
                >
                  <Check className="ic" aria-hidden="true" /> {pending ? "Creating…" : `Create ${committable} record${committable === 1 ? "" : "s"}`}
                </button>
              </>
            )}
          </div>
        </>
      ) : null}
    </Drawer>
  );
}

// ---- Header CTA group rendered inside the page header (replaces the disabled buttons) ----
export function HerdActions({
  parks,
  sheds,
  farms,
  locationsAvailable,
  idempotencyKey,
  returnTo,
}: {
  parks: LocationOption[];
  sheds: LocationOption[];
  farms: LocationOption[];
  locationsAvailable: boolean;
  idempotencyKey: string;
  returnTo: string;
}) {
  const [openDrawer, setOpenDrawer] = useState<"register" | "bulk" | null>(null);

  return (
    <>
      <button type="button" className="btn" onClick={() => setOpenDrawer("bulk")}>
        <Upload className="ic" style={{ width: 13 }} aria-hidden="true" /> Import sheet
      </button>
      <button type="button" className="btn p" onClick={() => setOpenDrawer("register")}>
        <Plus className="ic" aria-hidden="true" /> Register goat
      </button>

      <RegisterGoatDrawer
        open={openDrawer === "register"}
        onClose={() => setOpenDrawer(null)}
        parks={parks}
        sheds={sheds}
        farms={farms}
        locationsAvailable={locationsAvailable}
        idempotencyKey={idempotencyKey}
        returnTo={returnTo}
      />
      <BulkImportDrawer open={openDrawer === "bulk"} onClose={() => setOpenDrawer(null)} />
    </>
  );
}
