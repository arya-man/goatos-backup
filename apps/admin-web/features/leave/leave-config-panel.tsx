"use client";

import { useState, useTransition } from "react";

import { control, controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LeaveApprovalConfig } from "@/lib/api/server";
import { saveLeaveConfigAction } from "./actions";

/**
 * Who approves leave (maintainer decision 2026-09-10: "keep it as a feature flag in HRM, only I
 * should set whom it should go to"). Two ticks -- park head, HR -- and every ticked approver must
 * accept. Gated by the compiled `leave_config` control (leave.approval.configure, CEO only): a
 * reader sees the ticks disabled with the backend's reason, never a role-string check here.
 * Local state until Save; the save goes through a Server Action and the panel re-renders from
 * what the backend read back.
 */
export function LeaveConfigPanel({
  config,
  pageContract,
}: {
  config: LeaveApprovalConfig;
  pageContract: AdminUiPageContract;
}) {
  const t = (key: string) => copy(pageContract, key);
  const canEdit = controlEnabled(pageContract, "leave_config", false);
  const disabledReason = canEdit ? "" : (control(pageContract, "leave_config").disabled_reason ?? "");
  const [parkHead, setParkHead] = useState(config.park_head_required);
  const [hr, setHr] = useState(config.hr_required);
  const [rowVersion, setRowVersion] = useState(config.row_version);
  const [message, setMessage] = useState<{ tone: "ok" | "dng"; text: string } | null>(null);
  const [pending, startTransition] = useTransition();
  const dirty = parkHead !== config.park_head_required || hr !== config.hr_required;

  const onSave = () => {
    setMessage(null);
    startTransition(async () => {
      const result = await saveLeaveConfigAction({ park_head_required: parkHead, hr_required: hr, row_version: rowVersion });
      if (result.ok) {
        setRowVersion(result.config.row_version);
        setMessage({ tone: "ok", text: t("config.saved") });
      } else {
        setMessage({ tone: "dng", text: result.message });
      }
    });
  };

  return (
    <section className="card" data-testid="leave-config" style={{ marginBottom: 16 }}>
      <div className="chead">
        <h3>{t("config.title")}</h3>
      </div>
      <div className="cbody">
        <p className="small muted" style={{ marginTop: 0 }}>
          {t("config.help")}
        </p>
        <div style={{ display: "flex", gap: 20, flexWrap: "wrap", alignItems: "center" }}>
          <label className="small" style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <input
              type="checkbox"
              name="park_head_required"
              checked={parkHead}
              disabled={!canEdit || pending}
              onChange={(e) => setParkHead(e.target.checked)}
            />
            {t("config.park_head")}
          </label>
          <label className="small" style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <input type="checkbox" name="hr_required" checked={hr} disabled={!canEdit || pending} onChange={(e) => setHr(e.target.checked)} />
            {t("config.hr")}
          </label>
          <button
            type="button"
            className="btn primary"
            disabled={!canEdit || pending || !dirty || (!parkHead && !hr)}
            onClick={onSave}
            title={disabledReason || undefined}
          >
            {t("config.save")}
          </button>
        </div>
        {!canEdit && disabledReason ? (
          <div className="small muted" style={{ marginTop: 8 }}>
            {disabledReason}
          </div>
        ) : null}
        {message ? (
          <div className={`small ${message.tone === "ok" ? "" : "muted"}`} style={{ marginTop: 8, color: message.tone === "dng" ? "var(--dng)" : undefined }}>
            {message.text}
          </div>
        ) : null}
      </div>
    </section>
  );
}
