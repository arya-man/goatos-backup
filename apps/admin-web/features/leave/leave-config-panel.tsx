"use client";

import { useState, useTransition } from "react";
import { UsersRound } from "lucide-react";
import Box from "@mui/material/Box";

import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
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
    <Card data-testid="leave-config" sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader
        sx={{ p: 0, mb: 2, alignItems: "center" }}
        title={
          <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
            <UsersRound className="ic" style={{ width: 18, color: "var(--primary)" }} aria-hidden="true" />
            {t("config.title")}
          </span>
        }
        subheader={t("config.help")}
      />
      <Box sx={{ display: "flex", gap: 2.5, flexWrap: "wrap", alignItems: "center" }}>
        <FormControlLabel
          disabled={!canEdit || pending}
          control={
            <Checkbox
              name="park_head_required"
              checked={parkHead}
              onChange={(e) => setParkHead(e.target.checked)}
              sx={{ p: { xs: 1.5, sm: 1 } }}
            />
          }
          label={t("config.park_head")}
        />
        <FormControlLabel
          disabled={!canEdit || pending}
          control={<Checkbox name="hr_required" checked={hr} onChange={(e) => setHr(e.target.checked)} sx={{ p: { xs: 1.5, sm: 1 } }} />}
          label={t("config.hr")}
        />
        {/* Save appears once there is something to save (TR2-P2-4; guard: leave-save-when-dirty): a
            permanently grey disabled Save read as a dead control. A reader who cannot edit sees the
            backend reason line below instead. */}
        {canEdit && (dirty || pending) ? (
          <Button
            variant="contained"
            color="primary"
            size="small"
            loading={pending}
            disabled={!parkHead && !hr}
            onClick={onSave}
          >
            {t("config.save")}
          </Button>
        ) : null}
      </Box>
      {!canEdit && disabledReason ? (
        <div className="small muted" style={{ marginTop: 10 }}>
          {disabledReason}
        </div>
      ) : null}
      {message ? (
        <div
          role="status"
          className="small"
          style={{ marginTop: 10, color: message.tone === "ok" ? "var(--success-ink)" : "var(--error-ink)" }}
        >
          {message.text}
        </div>
      ) : null}
    </Card>
  );
}
