"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ArrowUpRight, Plus, Upload, X } from "lucide-react";
import { scopeHref, type Scope } from "@/lib/scope";

// PHC · Vaccination header actions (mock: SOP · Import sheet · New drive).
//
// Backend honesty (the maintainer's rule): admin-web has NO create-drive or bulk-import mutation for
// vaccination. A drive is NOT created here — it is the downstream effect of publishing a source-backed
// protocol rule in Config (obligations generate, the sweeper batches a shed drive + SOP task). Supplier /
// Holding-Farm dose evidence is imported under Procurement · Source Entry. So these two buttons keep the
// mock's shape but the drawers are READ-ONLY GUIDANCE that route to the real authoring surfaces — no editable
// fields that discard input, no fake CSV preview-to-submit, and no green CTA implying a backend write.

const IMPORT_REFERENCE_COLUMNS = [
  "drive_code",
  "protocol_code",
  "park",
  "cohort",
  "shed",
  "vaccine",
  "due_date",
  "animals",
  "proof_type",
  "owner_role",
];

function Drawer({
  open,
  onClose,
  title,
  subtitle,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  subtitle?: string;
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
    <>
      <div role="presentation" className="veil" onClick={onClose} aria-hidden="true" />
      <aside
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        className="drawer on"
      >
        <div className="dh">
          <div>
            <div className="mt">VACCINE</div>
            <h2>{title}</h2>
            {subtitle ? <div className="muted small" style={{ marginTop: 2 }}>{subtitle}</div> : null}
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button type="button" className="iconbtn" onClick={onClose} aria-label="Close">
            <X className="ic" aria-hidden="true" />
          </button>
        </div>
        <div className="dc">{children}</div>
      </aside>
    </>
  );
}

export function VaccinationHeaderActions({ scope }: { scope: Scope }) {
  const [openDrawer, setOpenDrawer] = useState<"import" | "new-drive" | null>(null);
  const configHref = scopeHref("/config", scope, {}, { category: "vaccination" });
  const sopsHref = scopeHref("/sops", scope);
  const actionCenterHref = scopeHref("/action-center", scope, {}, { state: "owner_missing" });
  const sourceEntryHref = scopeHref("/procurement/source-entry", scope);

  return (
    <>
      <button type="button" className="btn" onClick={() => setOpenDrawer("import")}>
        <Upload className="ic" aria-hidden="true" /> Import sheet
      </button>
      <button type="button" className="btn p" onClick={() => setOpenDrawer("new-drive")}>
        <Plus className="ic" aria-hidden="true" /> New drive
      </button>

      <Drawer
        open={openDrawer === "import"}
        onClose={() => setOpenDrawer(null)}
        title="Import vaccination sheet"
        subtitle="Where vaccination drives and dose history actually enter Goat OS."
      >
        <div className="note" style={{ marginBottom: 14 }}>
          There is <b>no in-app bulk drive importer</b> on this surface — admin-web never writes vaccination
          state directly. Drives are generated from config, and supplier dose history is imported under Source
          Entry. Use the real paths below; nothing on this drawer submits.
        </div>

        <div className="metagrid" style={{ marginBottom: 14 }}>
          <div>
            <div className="k">New drives</div>
            <div className="v">Publish a source-backed protocol rule in Config → obligations generate → the sweeper batches a shed drive.</div>
          </div>
          <div>
            <div className="k">Supplier / HF dose history</div>
            <div className="v">Import & review Holding-Farm vaccination evidence under Procurement · Source Entry.</div>
          </div>
        </div>

        <div className="fld" aria-disabled="true">
          <label>Drive sheet columns (reference)</label>
          <div className="note mono" style={{ overflowX: "auto" }}>
            {IMPORT_REFERENCE_COLUMNS.join(", ")}
          </div>
          <div className="muted small" style={{ marginTop: 4, lineHeight: 1.45 }}>
            Reference only. The committed importer/contract is not built for this slice, so no upload control is
            shown rather than a fake preview-to-submit.
          </div>
        </div>

        <div className="df" style={{ display: "flex", gap: 10, justifyContent: "flex-end", flexWrap: "wrap" }}>
          <button type="button" className="btn" onClick={() => setOpenDrawer(null)}>
            Close
          </button>
          <Link href={sourceEntryHref} className="btn">
            Source Entry <ArrowUpRight className="ic" style={{ width: 14 }} aria-hidden="true" />
          </Link>
          <Link href={configHref} className="btn p">
            Open Config <ArrowUpRight className="ic" style={{ width: 14 }} aria-hidden="true" />
          </Link>
        </div>
      </Drawer>

      <Drawer
        open={openDrawer === "new-drive"}
        onClose={() => setOpenDrawer(null)}
        title="New vaccination drive"
        subtitle="A drive is generated from config — it is not hand-created here."
      >
        <div className="note" style={{ marginBottom: 14 }}>
          A vaccination drive is the downstream effect of a <b>published protocol rule</b>, not a form on this
          screen. This drawer explains the mechanic and links to the real authoring surface; nothing here
          submits or is saved.
        </div>

        <div className="chain" tabIndex={0} role="group" aria-label="How a new drive is generated" style={{ marginBottom: 14 }}>
          {[
            ["Target", "Drive cohort", "cohort · age · park"],
            ["Group", "per-shed events", "all matching goats grouped by shed"],
            ["Route", "shed owner", "manager / assistant assignment"],
            ["Execute", "video per shed", "proof gates before consume"],
          ].map(([step, title, detail]) => (
            <div className="cstep" key={step}>
              <div className="s">{step}</div>
              <b>{title}</b>
              <div className="d">{detail}</div>
            </div>
          ))}
        </div>

        <div className="metagrid" style={{ marginBottom: 14 }}>
          <div>
            <div className="k">1 · Publish rule</div>
            <div className="v">Config → vaccination category → publish a source-backed protocol version.</div>
          </div>
          <div>
            <div className="k">2 · Generation</div>
            <div className="v">Obligations materialize per eligible goat; the sweeper batches them into a per-shed drive + SOP task.</div>
          </div>
          <div>
            <div className="k">3 · Assign / act</div>
            <div className="v">Owner gaps and execution work surface in the Action Center.</div>
          </div>
        </div>

        <div className="df" style={{ display: "flex", gap: 10, justifyContent: "flex-end", flexWrap: "wrap" }}>
          <button type="button" className="btn" onClick={() => setOpenDrawer(null)}>
            Cancel
          </button>
          <Link href={sopsHref} className="btn">
            SOP Library
          </Link>
          <Link href={actionCenterHref} className="btn">
            Action Center
          </Link>
          <Link href={configHref} className="btn p">
            Open Config <ArrowUpRight className="ic" style={{ width: 14 }} aria-hidden="true" />
          </Link>
        </div>
      </Drawer>
    </>
  );
}
