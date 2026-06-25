"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { BookOpen, Check, Clock, Video, X } from "lucide-react";
import type { SopCardView } from "@/features/sops";

export interface VaccinationSopQuickViewProps {
  // The linked vaccination SOP, derived on the server from the real /admin/sops data. Used here only for the
  // title/version chip; the quick-view shows the vaccination drive lifecycle regardless of DB authoring state
  // (a published form_dsl is the authoring detail, surfaced in full via the SOP Library link).
  view: SopCardView | null;
  error?: { code?: string; message: string } | null;
  authRequired?: boolean;
}

// The vaccination drive SOP lifecycle (ported verbatim from the mock SOP quick-view, SOPS.vacc). This is the
// operator-facing process preview — what a drive does end to end — not the raw form_dsl fields.
const DRIVE_SOP_STEPS: Array<{ title: string; detail: string; done?: boolean; current?: boolean; videoProof?: boolean }> = [
  { title: "Drive scheduled", detail: "Cohort + vaccine; FEFO stock reserved.", done: true },
  { title: "Per-shed administration", detail: "Dose per animal; video proof per shed event.", current: true, videoProof: true },
  { title: "Consume posted (ledger)", detail: "Verified completion posts a consume movement for doses (FEFO)." },
  { title: "Coverage + booster", detail: "Coverage % computed; next booster scheduled." },
];

// PHC · Vaccination header "SOP" CTA. Opens a compact, mock-faithful Vaccination Drive SOP quick-view in a
// modal WITHOUT leaving /vaccination (local state, no navigation). The full library / versioning / authoring
// lives at /sops, reached via the secondary "Open in SOP Library" link — this is the in-context preview.
export function VaccinationSopButton({ view }: VaccinationSopQuickViewProps) {
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
      {open ? <VaccinationSopModal view={view} onClose={() => setOpen(false)} /> : null}
    </>
  );
}

function VaccinationSopModal({ onClose }: { view: SopCardView | null; onClose: () => void }) {
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
        <div className="cmh">
          <span
            className="fic"
            style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}
          >
            <BookOpen className="ic" />
          </span>
          <div>
            <div className="mono muted" style={{ fontSize: 11 }}>
              PHC · VACCINATION
            </div>
            <div className="b700">Vaccination Drive SOP</div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="x" onClick={onClose} aria-label="Close">
            <X className="ic" />
          </button>
        </div>

        <div className="cmb" style={{ display: "block" }}>
          <div className="note" style={{ marginBottom: 14 }}>
            <Clock className="ic" style={{ width: 14, verticalAlign: -2 }} aria-hidden="true" /> Per protocol window ·
            booster intervals tracked
          </div>

          <div className="stepper">
            {DRIVE_SOP_STEPS.map((s, i) => (
              <div className={`step${s.done ? " done" : ""}${s.current ? " cur" : ""}`} key={s.title}>
                <div className="ln" />
                <div className="no">
                  {s.done ? <Check className="ic" style={{ width: 14, strokeWidth: 2.4 }} aria-hidden="true" /> : i + 1}
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
