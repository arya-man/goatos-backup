"use client";

// Counts -> "Change stage". Retags every live animal in one pen.
//
// The flow is deliberately check-then-apply. This write has no approval step and no proof behind
// it, so the preview IS the safety mechanism: the operator sees the pen's current composition and
// the exact number of animals that would move before the button that applies it becomes available.
//
// Every visible string comes from the page contract. This component owns layout, open/closed state,
// and the selected pen -- nothing else. It never composes the operational-location label (the
// backend sends `operational_location_display`), never derives counts, and never decides authority.
import { useMemo, useState, useTransition } from "react";
import { copy, optionalCopy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  ReclassifyShedStagePreviewResponse,
  ReclassifyShedStageResponse,
} from "@/lib/api/server";
import { Drawer } from "./herd-actions-ui";
import { commitShedStageAction, previewShedStageAction } from "./shed-stage-actions";

// PenOption is one selectable operational location. It comes from the partition CATALOG, never from
// census facets: a pen that currently holds zero animals still exists and must stay selectable, and
// a write picker answers "where may work be directed", not "where animals happen to be".
export type PenOption = {
  key: string;
  shedId: string;
  partitionLabel: string;
  label: string;
};

// StageOption is the tenant's active stage vocabulary. Business-managed rows in Postgres, never a
// constant list here -- adding a cohort tag must not need a frontend release.
//
// `band` is what an animal INHERITS when its pen is retagged ("kid"/"adult", or "" for a tag the
// farm has not classified), and `assignable` is false for a clinical tag the assigning writes
// reject. Both come from the backend; neither is derived from the code string here, because that
// would put a second copy of the clinical set in the frontend.
export type StageOption = {
  code: string;
  // label IS the stage code ("F2-Male", "Non-Pregnant", "Buck"). That is the tag the farm uses, the
  // value stored on the animal and on the pen, and the string every Counts screen renders -- so it
  // is what a picker must offer. Showing the descriptive name instead ("Fattening male") made an
  // operator pick one word and watch a different one appear in the cell.
  label: string;
  // description is the lookup's human name, shown as secondary context only when it says something
  // the code does not. Blank when the two are the same word (Buck, Mother, Pregnant).
  description: string;
  band: string;
  assignable: boolean;
};

type Phase =
  | { kind: "editing" }
  | { kind: "checked"; preview: ReclassifyShedStagePreviewResponse }
  | { kind: "done"; result: ReclassifyShedStageResponse }
  | { kind: "failed"; message: string };

export function ShedStageDrawer({
  pageContract,
  pens,
  stages,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  pens: PenOption[];
  stages: StageOption[];
  enabled: boolean;
  disabledReason: string;
}) {
  const [open, setOpen] = useState(false);
  const [penKey, setPenKey] = useState("");
  const [stage, setStage] = useState("");
  const [reason, setReason] = useState("");
  const [phase, setPhase] = useState<Phase>({ kind: "editing" });
  const [pending, startTransition] = useTransition();

  // One key per INTENT, not per attempt: it is minted when a checked preview is accepted and held
  // across retries, so a double-click or a retried network failure replays the first write rather
  // than emitting a second round of stage-change events.
  const [commitKey, setCommitKey] = useState("");

  const pen = useMemo(() => pens.find((option) => option.key === penKey), [pens, penKey]);

  // Any edit invalidates a preview that was taken against different inputs. Without this, an
  // operator could check pen A and apply to pen B.
  const resetCheck = () => {
    setPhase({ kind: "editing" });
    setCommitKey("");
  };

  const close = () => {
    setOpen(false);
    setPenKey("");
    setStage("");
    setReason("");
    setPhase({ kind: "editing" });
    setCommitKey("");
  };

  const check = () => {
    if (!pen || !stage) return;
    startTransition(async () => {
      const result = await previewShedStageAction({
        shed_id: pen.shedId,
        partition_label: pen.partitionLabel || undefined,
        management_stage: stage,
      });
      if (!result.ok) {
        setPhase({ kind: "failed", message: result.error.message });
        return;
      }
      setPhase({ kind: "checked", preview: result.data });
    });
  };

  const apply = () => {
    if (!pen || !stage || phase.kind !== "checked") return;
    const key = commitKey || crypto.randomUUID();
    setCommitKey(key);
    startTransition(async () => {
      const result = await commitShedStageAction(
        {
          shed_id: pen.shedId,
          partition_label: pen.partitionLabel || undefined,
          management_stage: stage,
          reason,
        },
        key,
      );
      if (!result.ok) {
        setPhase({ kind: "failed", message: result.error.message });
        return;
      }
      setPhase({ kind: "done", result: result.data });
    });
  };

  const reasonValid = reason.trim().length >= 3 && reason.trim().length <= 500;
  // Nothing to apply when every animal already carries the target tag. Saying so beats a button
  // that succeeds and reports zero.
  const nothingToChange = phase.kind === "checked" && phase.preview.changing === 0;

  return (
    <>
      <button
        type="button"
        className="btn p"
        disabled={!enabled}
        title={enabled ? undefined : disabledReason}
        onClick={() => setOpen(true)}
      >
        {copy(pageContract, "stage_change.title")}
      </button>

      <Drawer
        open={open}
        onClose={close}
        closeLabel={copy(pageContract, "stage_change.close_action")}
        title={copy(pageContract, "stage_change.heading")}
        subtitle={copy(pageContract, "stage_change.caption")}
        width={640}
      >
        <div style={{ display: "grid", gap: 14 }}>
          <label style={{ display: "grid", gap: 4 }}>
            <span className="small">{copy(pageContract, "stage_change.shed_label")}</span>
            <select
              value={penKey}
              onChange={(event) => {
                setPenKey(event.target.value);
                resetCheck();
              }}
            >
              <option value="">{copy(pageContract, "filter.all_option")}</option>
              {pens.map((option) => (
                <option key={option.key} value={option.key}>
                  {option.label}
                </option>
              ))}
            </select>
            <span className="muted small">{copy(pageContract, "stage_change.shed_hint")}</span>
          </label>

          <label style={{ display: "grid", gap: 4 }}>
            <span className="small">{copy(pageContract, "stage_change.stage_label")}</span>
            <select
              value={stage}
              onChange={(event) => {
                setStage(event.target.value);
                resetCheck();
              }}
            >
              <option value="">{copy(pageContract, "filter.all_option")}</option>
              {stages.map((option) => (
                <option key={option.code} value={option.code}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>

          <label style={{ display: "grid", gap: 4 }}>
            <span className="small">{copy(pageContract, "stage_change.reason_label")}</span>
            <input value={reason} onChange={(event) => setReason(event.target.value)} maxLength={500} />
            <span className="muted small">{copy(pageContract, "stage_change.reason_hint")}</span>
          </label>

          {phase.kind === "failed" ? (
            <div className="alert">{phase.message}</div>
          ) : null}

          {phase.kind === "checked" ? (
            <PreviewBlock pageContract={pageContract} preview={phase.preview} />
          ) : null}

          {phase.kind === "done" ? (
            <div className="alert" role="status">
              <b>{copy(pageContract, "stage_change.done")}</b>{" "}
              {phase.result.operational_location_display} · {phase.result.management_stage} ·{" "}
              {phase.result.reclassified}
            </div>
          ) : null}

          <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "center" }}>
            {phase.kind === "done" ? (
              <button type="button" className="btn p" onClick={close}>
                {copy(pageContract, "stage_change.close_action")}
              </button>
            ) : (
              <>
                <button
                  type="button"
                  className="btn"
                  onClick={check}
                  disabled={pending || !pen || !stage}
                  aria-busy={pending}
                >
                  {copy(pageContract, "stage_change.preview_action")}
                </button>
                <button
                  type="button"
                  className="btn p"
                  onClick={apply}
                  disabled={pending || phase.kind !== "checked" || nothingToChange || !reasonValid}
                  aria-busy={pending}
                >
                  {copy(pageContract, "stage_change.submit_action")}
                </button>
                <button type="button" className="btn" onClick={close} disabled={pending}>
                  {copy(pageContract, "stage_change.cancel_action")}
                </button>
              </>
            )}
          </div>

          {phase.kind !== "done" ? (
            <div className="muted small">{copy(pageContract, "stage_change.applies_now")}</div>
          ) : null}
        </div>
      </Drawer>
    </>
  );
}

// PreviewBlock renders the backend's whole-scope answer verbatim. The three counts and the
// composition rows are all backend-owned; nothing here is recomputed from the others, so a mixed
// pen shows the mix rather than a number that looks tidy.
function PreviewBlock({
  pageContract,
  preview,
}: {
  pageContract: AdminUiPageContract;
  preview: ReclassifyShedStagePreviewResponse;
}) {
  const ageLabel =
    preview.age_band === "kid"
      ? copy(pageContract, "stage_change.age_kid")
      : preview.age_band === "adult"
        ? copy(pageContract, "stage_change.age_adult")
        : copy(pageContract, "stage_change.age_unchanged");

  return (
    <section className="card" style={{ padding: 14, display: "grid", gap: 10 }}>
      <div>
        <b>{copy(pageContract, "stage_change.preview_title")}</b>
        <div className="muted small">
          {preview.operational_location_display} → {preview.management_stage} · {ageLabel}
        </div>
      </div>

      <div style={{ display: "flex", gap: 18, flexWrap: "wrap" }}>
        <Stat label={copy(pageContract, "stage_change.total_label")} value={preview.total_live} />
        <Stat label={copy(pageContract, "stage_change.changing_label")} value={preview.changing} />
        <Stat label={copy(pageContract, "stage_change.unchanged_label")} value={preview.unchanged} />
      </div>

      {preview.changing === 0 ? (
        <div className="muted small">{copy(pageContract, "stage_change.no_change")}</div>
      ) : null}

      <div>
        <div className="small" style={{ marginBottom: 4 }}>
          {copy(pageContract, "stage_change.current_title")}
        </div>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {preview.current_stages.map((bucket) => (
            <span key={`${bucket.management_stage}:${bucket.age_band}`} className="chip">
              {bucket.management_stage || optionalCopy(pageContract, "label.unassigned_stage") || "—"} ·{" "}
              {bucket.count}
            </span>
          ))}
        </div>
      </div>
    </section>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <div className="muted small">{label}</div>
      <div style={{ fontSize: 20, fontWeight: 600 }}>{value}</div>
    </div>
  );
}
