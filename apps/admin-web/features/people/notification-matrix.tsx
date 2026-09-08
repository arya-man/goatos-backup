"use client";

import { AlertTriangle, BellRing, RotateCcw } from "lucide-react";
import { useCallback, useMemo, useState, useTransition } from "react";

import { controlEnabled, control, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { NotificationAudienceMatrix } from "@/lib/api/server";
import { saveNotificationAudienceAction } from "./notification-actions";
import { groupAlertsByModule, isDirty, toggleDesignation, type MatrixAlert } from "./notification-matrix-rows";

/**
 * The Notifications matrix (maintainer decision 2026-09-08): one row per configurable alert,
 * one column per DESIGNATION, a tick where that job title receives the alert.
 *
 * Every visible word -- alert names, blurbs, module headings, designation labels, the chips and
 * every message -- comes from the backend contract or the matrix payload. The raw vocabulary
 * behind these ticks is `pc_director` and `feed.low_stock`, and neither is a word to put in front
 * of someone deciding who hears an alert.
 *
 * Local state only until Save on THAT row. Ticking must not navigate or re-render the page
 * beneath, so every control here is a button or a checkbox with a local handler; the save goes
 * through a Server Action and the row is replaced with what the backend read back.
 */

type RowState = {
  alert: MatrixAlert;
  draft: string[];
  pending: boolean;
  message: { tone: "ok" | "dng"; text: string } | null;
};

export function NotificationMatrix({
  matrix,
  pageContract,
}: {
  matrix: NotificationAudienceMatrix;
  pageContract: AdminUiPageContract;
}) {
  const t = useCallback((key: string) => copy(pageContract, key), [pageContract]);
  // The write is capability-gated through the compiled control: disabled with the backend's
  // reason for a reader, never a role-string check here.
  const canEdit = controlEnabled(pageContract, "edit_notifications", false);
  const disabledReason = canEdit ? "" : control(pageContract, "edit_notifications").disabled_reason ?? "";

  const [rows, setRows] = useState<Record<string, RowState>>(() => {
    const out: Record<string, RowState> = {};
    for (const alert of matrix.alerts) {
      out[alert.key] = { alert, draft: [...alert.designations], pending: false, message: null };
    }
    return out;
  });
  const [, startTransition] = useTransition();

  const groups = useMemo(() => groupAlertsByModule(matrix.modules, matrix.alerts), [matrix]);

  const patchRow = useCallback((key: string, patch: Partial<RowState> | ((row: RowState) => Partial<RowState>)) => {
    setRows((prev) => {
      const row = prev[key];
      if (!row) return prev;
      const next = typeof patch === "function" ? patch(row) : patch;
      return { ...prev, [key]: { ...row, ...next } };
    });
  }, []);

  const onToggle = useCallback(
    (key: string, code: string) => {
      patchRow(key, (row) => ({ draft: toggleDesignation(row.draft, code), message: null }));
    },
    [patchRow],
  );

  const submit = useCallback(
    (key: string, useDefaults: boolean) => {
      const row = rows[key];
      if (!row || row.pending) return;
      patchRow(key, { pending: true, message: null });
      startTransition(async () => {
        const result = await saveNotificationAudienceAction({
          alertKey: key,
          body: {
            designation_codes: useDefaults ? [] : row.draft,
            use_defaults: useDefaults,
            row_version: row.alert.row_version,
          },
        });
        if (!result.ok) {
          patchRow(key, { pending: false, message: { tone: "dng", text: result.message } });
          return;
        }
        patchRow(key, {
          alert: result.row,
          draft: [...result.row.designations],
          pending: false,
          message: { tone: "ok", text: t("notifications.saved") },
        });
      });
    },
    [rows, patchRow, t],
  );

  if (matrix.alerts.length === 0) {
    return <div className="empty">{t("notifications.empty")}</div>;
  }

  return (
    <section className="notification-matrix" aria-label={t("notifications.column.alert")}>
      <p className="sub" style={{ marginTop: 8 }}>
        <BellRing size={14} aria-hidden="true" style={{ verticalAlign: "-2px", marginRight: 6 }} />
        {t("notifications.intro")}
      </p>
      <p className="sub" style={{ marginTop: 2 }}>
        {t("notifications.always_told")}
      </p>
      {!canEdit && disabledReason ? (
        <div className="callout warn" role="note" style={{ marginTop: 10 }}>
          <AlertTriangle size={14} aria-hidden="true" /> {disabledReason}
        </div>
      ) : null}

      <div className="tblwrap" style={{ marginTop: 12, overflowX: "auto" }}>
        <table className="people-table notification-matrix-table" style={{ tableLayout: "fixed", width: "100%", minWidth: 960 }}>
          <colgroup>
            <col style={{ width: 270 }} />
            {matrix.designations.map((d) => (
              <col key={d.code} />
            ))}
          </colgroup>
          <thead>
            <tr>
              <th>{t("notifications.column.alert")}</th>
              {matrix.designations.map((d) => (
                <th
                  key={d.code}
                  title={d.grade ? d.grade : undefined}
                  style={{ textAlign: "center", whiteSpace: "normal", textTransform: "none", fontSize: 10.5, lineHeight: 1.25, padding: "8px 2px", verticalAlign: "bottom", overflowWrap: "normal" }}
                >
                  {d.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {groups.map((group) => (
              <GroupRows
                key={group.key}
                label={group.label}
                columns={matrix.designations.length + 1}
                alerts={group.alerts}
                rows={rows}
                designations={matrix.designations}
                canEdit={canEdit}
                t={t}
                onToggle={onToggle}
                onSave={(key) => submit(key, false)}
                onReset={(key) => submit(key, true)}
              />
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function GroupRows({
  label,
  columns,
  alerts,
  rows,
  designations,
  canEdit,
  t,
  onToggle,
  onSave,
  onReset,
}: {
  label: string;
  columns: number;
  alerts: MatrixAlert[];
  rows: Record<string, RowState>;
  designations: NotificationAudienceMatrix["designations"];
  canEdit: boolean;
  t: (key: string) => string;
  onToggle: (key: string, code: string) => void;
  onSave: (key: string) => void;
  onReset: (key: string) => void;
}) {
  return (
    <>
      <tr className="group-row">
        <th colSpan={columns} scope="colgroup" style={{ textAlign: "left", background: "var(--panel-2)" }}>
          {label}
        </th>
      </tr>
      {alerts.map((alert) => {
        const row = rows[alert.key];
        if (!row) return null;
        const dirty = isDirty(row.alert, row.draft);
        const nobody = row.draft.length === 0;
        return (
          <tr key={alert.key} data-alert-key={alert.key}>
            <td>
              <div style={{ fontWeight: 600 }}>{row.alert.label}</div>
              <div className="sub" style={{ marginTop: 2, whiteSpace: "normal" }}>
                {row.alert.blurb}
              </div>
              <div style={{ display: "flex", flexWrap: "wrap", gap: 6, alignItems: "center", marginTop: 6 }}>
                {row.alert.customised ? (
                  <span className="tag t-info">{t("notifications.chip.custom")}</span>
                ) : (
                  <span className="tag t-mut">{t("notifications.chip.default")}</span>
                )}
                {nobody ? <span className="tag t-warn">{t("notifications.chip.nobody")}</span> : null}
                {canEdit ? (
                  <>
                    <button
                      type="button"
                      className="btn b sm"
                      disabled={!dirty || row.pending}
                      onClick={() => onSave(alert.key)}
                    >
                      {row.pending ? t("notifications.action.saving") : t("notifications.action.save")}
                    </button>
                    {row.alert.customised ? (
                      <button
                        type="button"
                        className="btn ghost sm"
                        title={t("notifications.action.reset_hint")}
                        disabled={row.pending}
                        onClick={() => onReset(alert.key)}
                      >
                        <RotateCcw size={12} aria-hidden="true" /> {t("notifications.action.use_default")}
                      </button>
                    ) : null}
                  </>
                ) : null}
              </div>
              {row.message ? (
                <div className={`sub ${row.message.tone === "dng" ? "t-dng" : "t-ok"}`} role="status" style={{ marginTop: 4 }}>
                  {row.message.text}
                </div>
              ) : null}
            </td>
            {designations.map((d) => {
              const ticked = row.draft.includes(d.code);
              return (
                <td key={d.code} style={{ textAlign: "center", padding: "6px 4px" }}>
                  <input
                    type="checkbox"
                    aria-label={`${row.alert.label}: ${d.label}`}
                    checked={ticked}
                    disabled={!canEdit || row.pending}
                    onChange={() => onToggle(alert.key, d.code)}
                  />
                </td>
              );
            })}
          </tr>
        );
      })}
    </>
  );
}
