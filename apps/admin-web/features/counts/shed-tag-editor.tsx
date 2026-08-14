"use client";

// Counts Breakdown -> the inline tag editor on the Stage cell.
//
// DOUBLE-CLICK a row's tag, type to narrow the tenant's stage vocabulary, pick one, confirm, and
// the write retags every live animal in that row's PEN and records the pen's own tag -- one backend
// transaction, one permission, the same endpoint the removed drawer used.
//
// SCOPE IS THE PEN, NOT THE ROW, and the two are genuinely different: a breakdown row is a census
// SLICE (one breed and one sex inside a pen), while the write moves the whole pen. So the confirm
// step reports the PEN's own animal total, taken from the preview, never the row's count -- an
// operator retagging a 73-animal row must see that 200 animals are about to move. Sibling rows for
// other breeds and sexes in that pen change with it, which is why the page revalidates after.
//
// Double-click rather than single: every cell in this table is a census figure an operator reads,
// and a single click that opened an editor would fire constantly while scanning the page. Enter and
// Space open it too, so the control is reachable without a mouse.
//
// CHECK-THEN-APPLY is kept. The endpoint has no approval step and no proof behind it, so the
// preview IS the safety mechanism: picking a tag shows how many animals would move, and only then
// does the button that applies it appear. A pen holding nothing is not an error here -- it is an
// ordinary thing to configure ahead of animals arriving -- so the confirm step says so and sends
// `configure_empty`.
//
// Every visible string comes from the page contract, including the default reason that lands in the
// audit row. This component owns layout, open/closed state, the typed filter, and the operator's
// edit of that reason. It composes no copy, derives no counts, and decides no authority.
import { useEffect, useMemo, useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ReclassifyShedStagePreviewResponse } from "@/lib/api/server";

import { commitShedStageAction, previewShedStageAction, type StageOption } from "./shed-stage-actions";

// bandChanging reports whether the target tag's band differs from what any animal in the pen
// carries today. The preview's current_stages buckets are whole-pen and disjoint, so a single
// differing bucket means the confirm line is worth showing.
function bandChanging(preview: ReclassifyShedStagePreviewResponse): boolean {
  return (preview.current_stages ?? []).some((bucket) => bucket.age_band !== preview.age_band);
}

type Phase =
  | { kind: "closed" }
  | { kind: "picking" }
  | { kind: "checking"; stage: string }
  | { kind: "confirming"; stage: string; preview: ReclassifyShedStagePreviewResponse }
  | { kind: "failed"; message: string };

export function ShedTagEditor({
  pageContract,
  shedId,
  partitionLabel,
  currentTag,
  emptyLabel,
  stages,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  shedId: string;
  partitionLabel: string;
  currentTag: string;
  emptyLabel: string;
  stages: StageOption[];
  enabled: boolean;
  disabledReason: string;
}) {
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>({ kind: "closed" });
  const [filter, setFilter] = useState("");
  const [reason, setReason] = useState("");
  const [pending, startTransition] = useTransition();
  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // One key per INTENT, minted when a preview is accepted and held across retries, so a double
  // click or a retried network failure replays the first write instead of emitting a second round
  // of stage-change events.
  const [commitKey, setCommitKey] = useState("");

  const open = phase.kind !== "closed";

  // Outside click and Escape close the editor, matching every other same-page overlay in the app.
  // Local state only: this never navigates, so the row behind it is not re-fetched on open/close.
  useEffect(() => {
    if (!open) return undefined;
    const onPointerDown = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) close();
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  });

  useEffect(() => {
    if (phase.kind === "picking") inputRef.current?.focus();
  }, [phase.kind]);

  function close() {
    setPhase({ kind: "closed" });
    setFilter("");
    setCommitKey("");
  }

  // The kid/adult wording is contract copy, so the raw band token never reaches a screen.
  function bandLabel(band: string): string {
    if (band === "kid") return copy(pageContract, "action.retag.band_kid");
    if (band === "adult") return copy(pageContract, "action.retag.band_adult");
    return "";
  }

  // Substring match on both the human label and the stored code, because an operator who knows the
  // vocabulary types "f2" as readily as "Fattening".
  const matches = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return stages;
    return stages.filter(
      (option) => option.label.toLowerCase().includes(needle) || option.code.toLowerCase().includes(needle),
    );
  }, [filter, stages]);

  function pick(stage: string) {
    setPhase({ kind: "checking", stage });
    startTransition(async () => {
      const result = await previewShedStageAction({
        shed_id: shedId,
        ...(partitionLabel ? { partition_label: partitionLabel } : {}),
        management_stage: stage,
      });
      if (!result.ok) {
        setPhase({ kind: "failed", message: result.error.message || copy(pageContract, "action.retag.failed") });
        return;
      }
      setReason(copy(pageContract, "action.retag.default_reason"));
      setCommitKey(crypto.randomUUID());
      setPhase({ kind: "confirming", stage, preview: result.data });
    });
  }

  function apply(stage: string) {
    startTransition(async () => {
      const result = await commitShedStageAction(
        {
          shed_id: shedId,
          ...(partitionLabel ? { partition_label: partitionLabel } : {}),
          management_stage: stage,
          reason: reason.trim(),
          // An empty pen is a legitimate thing to configure from this screen, so the write is told
          // to accept one. A caller meaning "retag these animals" leaves it off and still gets the
          // wrong-pen refusal.
          configure_empty: true,
        },
        commitKey,
      );
      if (!result.ok) {
        setPhase({ kind: "failed", message: result.error.message || copy(pageContract, "action.retag.failed") });
        return;
      }
      close();
      // The row's tag, and the census on every sibling screen, are stale by construction now.
      router.refresh();
    });
  }

  if (!enabled) {
    // Disabled-with-reason, never hidden: the backend decides authority and says why, and a control
    // that comes and goes reads as a broken screen rather than a withheld one.
    return (
      <span title={disabledReason} aria-disabled="true">
        {currentTag || <span className="muted small">{emptyLabel}</span>}
      </span>
    );
  }

  return (
    <div ref={rootRef} className="tagedit">
      <button
        type="button"
        className="tagedit-value"
        onDoubleClick={() => (open ? close() : setPhase({ kind: "picking" }))}
        onKeyDown={(event) => {
          if (event.key !== "Enter" && event.key !== " ") return;
          event.preventDefault();
          if (open) close();
          else setPhase({ kind: "picking" });
        }}
        aria-expanded={open}
        aria-haspopup="dialog"
        title={copy(pageContract, "action.retag.hint")}
      >
        {currentTag ? <span className="tag">{currentTag}</span> : <span className="muted small">{emptyLabel}</span>}
      </button>

      {open ? (
        <div className="tagedit-pop" role="dialog" aria-label={copy(pageContract, "stage_change.title")}>
          {phase.kind === "picking" || phase.kind === "checking" ? (
            <>
              <input
                ref={inputRef}
                type="text"
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                placeholder={copy(pageContract, "action.retag.search_placeholder")}
                aria-label={copy(pageContract, "action.retag.search_placeholder")}
              />
              <div className="tagedit-list">
                {matches.length === 0 ? (
                  <div className="muted small tagedit-empty">{copy(pageContract, "action.retag.no_matches")}</div>
                ) : (
                  matches.map((option) => (
                    <button
                      key={option.code}
                      type="button"
                      className="tagedit-option"
                      disabled={pending}
                      onClick={() => pick(option.code)}
                    >
                      <span className="tagedit-option-name">
                        {/* The TAG itself -- the same string the cell will show once applied. */}
                        <span>{option.label}</span>
                        {/* The lookup's descriptive name, only where it says something the tag does
                            not -- several tags are already the whole answer on their own. */}
                        {option.description ? (
                          <span className="muted small">{option.description}</span>
                        ) : null}
                      </span>
                      {/* The band an animal INHERITS from this tag, shown in the list because a
                          cohort name does not otherwise announce that it moves kids to adults. */}
                      {bandLabel(option.band) ? (
                        <span className="muted small">{bandLabel(option.band)}</span>
                      ) : null}
                    </button>
                  ))
                )}
              </div>
              {phase.kind === "checking" ? (
                <div className="muted small tagedit-foot">{copy(pageContract, "action.retag.checking")}</div>
              ) : null}
            </>
          ) : null}

          {phase.kind === "confirming" ? (
            <div className="tagedit-confirm">
              {/* The location string is the backend's own composed display, and the count is the
                  preview's whole-pen total -- neither is derived here. */}
              <div className="tagedit-confirm-title">
                <b>{phase.stage}</b> · {phase.preview.operational_location_display}
              </div>
              <div className="muted small">
                {phase.preview.total_live === 0
                  ? copy(pageContract, "action.retag.empty_scope")
                  : `${phase.preview.total_live} ${
                      phase.preview.total_live === 1
                        ? copy(pageContract, "action.retag.animal_noun")
                        : copy(pageContract, "action.retag.animals_noun")
                    }`}
              </div>
              {/* The band the animals will INHERIT, taken from the preview rather than from the
                  picked option, so what is confirmed is what the backend resolved. Stated only when
                  it actually moves them: repeating the band they already carry is noise. */}
              {phase.preview.age_band && phase.preview.total_live > 0 && bandChanging(phase.preview) ? (
                <div className="small tagedit-band">
                  {copy(pageContract, "action.retag.becomes")} {bandLabel(phase.preview.age_band)}
                </div>
              ) : null}
              <label className="tagedit-reason">
                <span className="muted small">{copy(pageContract, "action.retag.reason_label")}</span>
                <input
                  type="text"
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  maxLength={500}
                  aria-label={copy(pageContract, "action.retag.reason_label")}
                />
              </label>
              <div className="tagedit-actions">
                <button type="button" className="btn sm" onClick={close} disabled={pending}>
                  {copy(pageContract, "action.retag.cancel")}
                </button>
                <button
                  type="button"
                  className="btn sm primary"
                  onClick={() => apply(phase.stage)}
                  // The backend requires 3..500 characters, so the button states that rule rather
                  // than letting the operator discover it as a server error.
                  disabled={pending || reason.trim().length < 3}
                >
                  {pending ? copy(pageContract, "action.retag.applying") : copy(pageContract, "action.retag.apply")}
                </button>
              </div>
            </div>
          ) : null}

          {phase.kind === "failed" ? (
            <div className="tagedit-confirm">
              <div className="small">{phase.message}</div>
              <div className="tagedit-actions">
                <button type="button" className="btn sm" onClick={() => setPhase({ kind: "picking" })}>
                  {copy(pageContract, "action.retag.cancel")}
                </button>
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
