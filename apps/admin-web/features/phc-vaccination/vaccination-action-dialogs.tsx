"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ArrowUpRight, Plus, Upload, X } from "lucide-react";
import { scopeHref, type Scope } from "@/lib/scope";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// PHC · Vaccination header actions (mock: SOP · Import sheet · New drive).
//
// Backend honesty (the maintainer's rule): admin-web has NO create-drive or bulk-import mutation for
// vaccination. A drive is NOT created here — it is the downstream effect of publishing a source-backed
// protocol rule in Config (obligations generate, the sweeper batches a shed drive + SOP task). Supplier /
// Holding-Farm dose evidence is imported under Procurement · Source Entry. So these two buttons keep the
// mock's shape but the drawers are READ-ONLY GUIDANCE that route to the real authoring surfaces — no editable
// fields that discard input, no fake CSV preview-to-submit, and no green CTA implying a backend write.

function Drawer({
  open,
  onClose,
  pageContract,
  title,
  subtitle,
  children,
}: {
  open: boolean;
  onClose: () => void;
  pageContract: AdminUiPageContract;
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
            <div className="mt">{copy(pageContract, "drawer.vaccination.eyebrow")}</div>
            <h2>{title}</h2>
            {subtitle ? <div className="muted small" style={{ marginTop: 2 }}>{subtitle}</div> : null}
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button type="button" className="iconbtn" onClick={onClose} aria-label={copy(pageContract, "action.close")}>
            <X className="ic" aria-hidden="true" />
          </button>
        </div>
        <div className="dc">{children}</div>
      </aside>
    </>
  );
}

function splitOptionTitle(title: string): { title: string; detail: string } {
  const [head, detail = ""] = title.split("|");
  return { title: head, detail };
}

export function VaccinationHeaderActions({ scope, pageContract }: { scope: Scope; pageContract: AdminUiPageContract }) {
  const [openDrawer, setOpenDrawer] = useState<"import" | "new-drive" | null>(null);
  const configHref = scopeHref("/config", scope, {}, { category: "vaccination" });
  const sopsHref = scopeHref("/sops", scope);
  const actionCenterHref = scopeHref("/action-center", scope, {}, { state: "owner_missing" });
  const sourceEntryHref = scopeHref("/procurement/source-entry", scope);
  const importColumns = optionGroup(pageContract, "vaccination_import_columns").map((column) => column.label);
  const newDriveSteps = optionGroup(pageContract, "new_drive_steps").map((step) => ({
    key: step.key,
    step: step.label,
    ...splitOptionTitle(step.title),
  }));

  return (
    <>
      <button type="button" className="btn" onClick={() => setOpenDrawer("import")}>
        <Upload className="ic" aria-hidden="true" /> {copy(pageContract, "action.import_sheet")}
      </button>
      <button type="button" className="btn p" onClick={() => setOpenDrawer("new-drive")}>
        <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.new_drive")}
      </button>

      <Drawer
        open={openDrawer === "import"}
        onClose={() => setOpenDrawer(null)}
        pageContract={pageContract}
        title={copy(pageContract, "drawer.import.title")}
        subtitle={copy(pageContract, "drawer.import.subtitle")}
      >
        <div className="note" style={{ marginBottom: 14 }}>
          {copy(pageContract, "drawer.import.note")}
        </div>

        <div className="metagrid" style={{ marginBottom: 14 }}>
          <div>
            <div className="k">{copy(pageContract, "drawer.import.new_drives_title")}</div>
            <div className="v">{copy(pageContract, "drawer.import.new_drives_body")}</div>
          </div>
          <div>
            <div className="k">{copy(pageContract, "drawer.import.hf_history_title")}</div>
            <div className="v">{copy(pageContract, "drawer.import.hf_history_body")}</div>
          </div>
        </div>

        <div className="fld" aria-disabled="true">
          <label>{copy(pageContract, "drawer.import.columns_label")}</label>
          <div className="note mono" style={{ overflowX: "auto" }}>
            {importColumns.join(", ")}
          </div>
          <div className="muted small" style={{ marginTop: 4, lineHeight: 1.45 }}>
            {copy(pageContract, "drawer.import.reference_only")}
          </div>
        </div>

        <div className="df" style={{ display: "flex", gap: 10, justifyContent: "flex-end", flexWrap: "wrap" }}>
          <button type="button" className="btn" onClick={() => setOpenDrawer(null)}>
            {copy(pageContract, "action.close")}
          </button>
          <Link href={sourceEntryHref} className="btn">
            {copy(pageContract, "action.source_entry")} <ArrowUpRight className="ic" style={{ width: 14 }} aria-hidden="true" />
          </Link>
          <Link href={configHref} className="btn p">
            {copy(pageContract, "action.open_config")} <ArrowUpRight className="ic" style={{ width: 14 }} aria-hidden="true" />
          </Link>
        </div>
      </Drawer>

      <Drawer
        open={openDrawer === "new-drive"}
        onClose={() => setOpenDrawer(null)}
        pageContract={pageContract}
        title={copy(pageContract, "drawer.new_drive.title")}
        subtitle={copy(pageContract, "drawer.new_drive.subtitle")}
      >
        <div className="note" style={{ marginBottom: 14 }}>
          {copy(pageContract, "drawer.new_drive.note")}
        </div>

        <div className="chain" tabIndex={0} role="group" aria-label={copy(pageContract, "drawer.new_drive.aria")} style={{ marginBottom: 14 }}>
          {newDriveSteps.map(({ key, step, title, detail }) => (
            <div className="cstep" key={key}>
              <div className="s">{step}</div>
              <b>{title}</b>
              <div className="d">{detail}</div>
            </div>
          ))}
        </div>

        <div className="metagrid" style={{ marginBottom: 14 }}>
          <div>
            <div className="k">{copy(pageContract, "drawer.new_drive.publish_rule_title")}</div>
            <div className="v">{copy(pageContract, "drawer.new_drive.publish_rule_body")}</div>
          </div>
          <div>
            <div className="k">{copy(pageContract, "drawer.new_drive.generation_title")}</div>
            <div className="v">{copy(pageContract, "drawer.new_drive.generation_body")}</div>
          </div>
          <div>
            <div className="k">{copy(pageContract, "drawer.new_drive.assign_title")}</div>
            <div className="v">{copy(pageContract, "drawer.new_drive.assign_body")}</div>
          </div>
        </div>

        <div className="df" style={{ display: "flex", gap: 10, justifyContent: "flex-end", flexWrap: "wrap" }}>
          <button type="button" className="btn" onClick={() => setOpenDrawer(null)}>
            {copy(pageContract, "action.cancel")}
          </button>
          <Link href={sopsHref} className="btn">
            {copy(pageContract, "action.sop_library")}
          </Link>
          <Link href={actionCenterHref} className="btn">
            {copy(pageContract, "action.action_center")}
          </Link>
          <Link href={configHref} className="btn p">
            {copy(pageContract, "action.open_config")} <ArrowUpRight className="ic" style={{ width: 14 }} aria-hidden="true" />
          </Link>
        </div>
      </Drawer>
    </>
  );
}
