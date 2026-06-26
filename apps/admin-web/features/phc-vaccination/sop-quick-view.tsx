"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { AlertTriangle, BookOpen, Check, Clock, Video, X } from "lucide-react";
import type { SopCardView } from "@/features/sops";
import { VACCINATION_DRIVE_SOP_STEPS } from "@/features/phc-vaccination/vaccination-sop-steps";

export interface VaccinationSopQuickViewProps {
  // The linked vaccination SOP, derived on the server from the real /admin/sops data. error/authRequired
  // carry a failed lookup so the modal surfaces it instead of rendering the preview as if all loaded.
  view: SopCardView | null;
  error?: { code?: string; message: string } | null;
  authRequired?: boolean;
}

// The vaccination drive SOP step flow is the single shared source (also used by the Action Center
// obligation drawer) — see vaccination-sop-steps.ts. In this in-context preview, the first step is shown
// done and the second current (a representative running drive); the live obligation drawer derives
// done/current from the real computed states instead.
const DRIVE_PREVIEW_DONE = 1;

// PHC · Vaccination header "SOP" CTA. Opens a compact, mock-faithful Vaccination Drive SOP quick-view in a
// modal WITHOUT leaving /vaccination (local state, no navigation). The full library / versioning / authoring
// lives at /sops, reached via the secondary "Open in SOP Library" link — this is the in-context preview.
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
}: {
  view: SopCardView | null;
  error?: { code?: string; message: string } | null;
  authRequired?: boolean;
  onClose: () => void;
}) {
  // .cmh / .cmb flex+grid are scoped to .cfgmodal in the theme, so this compact .modal lays out its own
  // header/body explicitly — otherwise the close button stacks under the title.
  return (
    <>
      <div className="scrim on" onClick={onClose} />
      <div
        className="modal on"
        style={{ width: "min(480px,94vw)" }}
        role="dialog"
        aria-modal="true"
        aria-label="Vaccination Drive SOP"
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            padding: "14px 18px",
            borderBottom: "1px solid var(--line2)",
          }}
        >
          <span
            className="fic"
            style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}
          >
            <BookOpen className="ic" />
          </span>
          <div style={{ minWidth: 0 }}>
            <div className="mono muted" style={{ fontSize: 11 }}>
              PHC · VACCINATION
            </div>
            <div className="b700">Vaccination Drive SOP</div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            style={{
              cursor: "pointer",
              color: "var(--muted)",
              width: 32,
              height: 32,
              display: "grid",
              placeItems: "center",
              borderRadius: 8,
              background: "transparent",
              border: "none",
              flex: "none",
            }}
          >
            <X className="ic" />
          </button>
        </div>

        <div style={{ padding: "16px 18px" }}>
          {authRequired ? (
            <div className="alert" style={{ margin: 0 }}>
              <AlertTriangle className="ic" aria-hidden="true" />
              <div>Couldn’t load the vaccination SOP — sign in with Google to load it (the SOP engine is tenant-scoped).</div>
            </div>
          ) : error ? (
            <div className="alert" style={{ margin: 0 }}>
              <AlertTriangle className="ic" aria-hidden="true" />
              <div>
                Couldn’t load the vaccination SOP.{error.code ? <> <b>{error.code}</b></> : null} {error.message}
              </div>
            </div>
          ) : (
            <>
              {view === null ? (
                <div className="note" style={{ marginBottom: 14 }}>
                  No vaccination SOP is authored yet — the steps below are the standard drive flow. Author one in the
                  SOP Library to attach proof gates and versioning.
                </div>
              ) : null}
              <div className="note" style={{ marginBottom: 14 }}>
                <Clock className="ic" style={{ width: 14, verticalAlign: -2 }} aria-hidden="true" /> Per protocol window ·
                booster intervals tracked
              </div>

              <div className="stepper">
                {VACCINATION_DRIVE_SOP_STEPS.map((s, i) => (
                  <div className={`step${i < DRIVE_PREVIEW_DONE ? " done" : ""}${i === DRIVE_PREVIEW_DONE ? " cur" : ""}`} key={s.title}>
                    <div className="ln" />
                    <div className="no">
                      {i < DRIVE_PREVIEW_DONE ? <Check className="ic" style={{ width: 14, strokeWidth: 2.4 }} aria-hidden="true" /> : i + 1}
                    </div>
                    <div className="ct">
                      <b>{s.title}</b>
                      <div className="d">{s.detail}</div>
                      {s.videoProof ? (
                        <div className="vp">
                          <span className="tag t-pur">
                            <Video className="ic" style={{ width: 12 }} aria-hidden="true" /> video proof required
                          </span>
                        </div>
                      ) : null}
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
        </div>

        <div className="cfgmf">
          <button type="button" className="btn" onClick={onClose}>
            Close
          </button>
          <div className="sp" style={{ flex: 1 }} />
          <Link href="/sops" className="lk" title="Versions, change history, and authoring live in the SOP Library">
            Open in SOP Library
          </Link>
        </div>
      </div>
    </>
  );
}
