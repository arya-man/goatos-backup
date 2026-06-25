"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { BookOpen, BookText, Check, Plus, Users, Video, X } from "lucide-react";
import type { SopCardView } from "@/features/sops";

export interface VaccinationSopQuickViewProps {
  // The linked vaccination SOP, derived on the server from the real /admin/sops data. Null when no
  // vaccination SOP exists yet; error/authRequired surface backend problems instead of faking content.
  view: SopCardView | null;
  error?: { code?: string; message: string } | null;
  authRequired?: boolean;
}

// PHC · Vaccination header "SOP" CTA. Opens the contextual Vaccination Drive SOP quick-view in a modal
// (ported from the mock's SOP quick-view) WITHOUT leaving /vaccination — no navigation, local state only.
// The full library, versioning, and authoring live at /sops; this is the in-context read. When no
// vaccination SOP is linked yet, the modal offers the two real next actions: open the library or create one.
export function VaccinationSopButton({ view, error, authRequired }: VaccinationSopQuickViewProps) {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open]);

  return (
    <>
      <button
        type="button"
        className="btn"
        title="Vaccination SOP policy"
        aria-haspopup="dialog"
        onClick={() => setOpen(true)}
      >
        <BookOpen className="ic" aria-hidden="true" /> SOP
      </button>
      {open ? (
        <VaccinationSopModal view={view} error={error} authRequired={authRequired} onClose={() => setOpen(false)} />
      ) : null}
    </>
  );
}

function VaccinationSopModal({
  view,
  error,
  authRequired,
  onClose,
}: VaccinationSopQuickViewProps & { onClose: () => void }) {
  return (
    <>
      <div className="cfgback on" onClick={onClose} />
      <div
        className="cfgmodal on"
        style={{ width: "min(720px,96vw)" }}
        role="dialog"
        aria-modal="true"
        aria-label="Vaccination Drive SOP"
      >
        <div className="cmh">
          <span
            className="fic"
            style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}
          >
            <BookText className="ic" />
          </span>
          <div>
            <div className="mono muted" style={{ fontSize: 11 }}>
              PHC · VACCINATION
            </div>
            <div className="b700">{view ? view.name : "Vaccination Drive SOP"}</div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="x" onClick={onClose} aria-label="Close">
            <X className="ic" />
          </button>
        </div>

        <div className="cmb" style={{ display: "block" }}>
          {authRequired ? (
            <div className="alert warn">
              <Users className="ic" />
              <div>Sign in with Google to load the vaccination SOP — the admin SOP engine is tenant-scoped.</div>
            </div>
          ) : error ? (
            <div className="alert warn">
              <X className="ic" />
              <div>
                {error.code ? <b>{error.code}&nbsp;</b> : null}
                {error.message}
              </div>
            </div>
          ) : view ? (
            <SopBody view={view} />
          ) : (
            <NoSopLinked />
          )}
        </div>

        <div className="cfgmf">
          <button type="button" className="btn" onClick={onClose}>
            Close
          </button>
          <div className="sp" style={{ flex: 1 }} />
          {view ? (
            <Link href="/sops" className="btn p" title="Full SOP Library — versions, change history, authoring">
              <BookText className="ic" /> Open in SOP Library
            </Link>
          ) : null}
        </div>
      </div>
    </>
  );
}

function SopBody({ view }: { view: SopCardView }) {
  return (
    <>
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
        Steps &amp; questions <span className="muted small">({view.fields.length})</span>
      </div>
      {view.fields.length > 0 ? (
        <div className="htl">
          {view.fields.map((f, i) => {
            const Icon = f.type === "video_proof" || f.type === "photo_proof" ? Video : Check;
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
          This SOP has no published version yet — no <span className="mono">form_dsl</span> steps to show. Publish a
          version in the SOP Library to fill the drive checklist.
        </div>
      )}
    </>
  );
}

function NoSopLinked() {
  return (
    <div style={{ textAlign: "center", padding: 24 }}>
      <BookText
        className="ic"
        aria-hidden="true"
        style={{ width: 24, height: 24, marginBottom: 10, color: "var(--brand)" }}
      />
      <h3 style={{ margin: 0, fontSize: 16 }}>No vaccination SOP linked yet</h3>
      <p className="muted" style={{ maxWidth: 560, margin: "8px auto 16px", lineHeight: 1.6, fontSize: 13 }}>
        Vaccination drives execute against a published vaccination SOP (proof gates, central verification,
        per-goat repeat). None exists yet. Author one in the SOP Library, then drives can carry its checklist.
      </p>
      <div style={{ display: "flex", gap: 8, justifyContent: "center", flexWrap: "wrap" }}>
        <Link href="/sops" className="btn">
          <BookText className="ic" /> Open SOP Library
        </Link>
        <Link href="/sops?new=1" className="btn p">
          <Plus className="ic" /> Create SOP
        </Link>
      </div>
    </div>
  );
}
