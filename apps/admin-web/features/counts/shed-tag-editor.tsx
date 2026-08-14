"use client";

// Counts Breakdown -> the Stage cell's editor: retag a PEN.
//
// SCOPE IS THE PEN, NOT THE ROW, and the two are genuinely different: a breakdown row is a census
// SLICE (one breed and one sex inside a pen), while this write moves every live animal in the pen
// and records the pen's own configured tag. So the confirm step reports the PEN's total, taken from
// the preview -- an operator retagging a 27-animal row must see that 49 animals are about to move.
// Sibling rows for other breeds and sexes change with it, which is why the page revalidates after.
//
// The interaction (double-click, type to narrow, confirm, apply) lives in InlineCellEditor, shared
// with the Breed and Gender cells. What stays here is what makes THIS cell different: the pen
// scope, the stage vocabulary, and the kid/adult consequence.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { InlineCellEditor, type InlineChoice, type InlinePreview } from "./inline-cell-editor";
import { commitShedStageAction, previewShedStageAction, type StageOption } from "./shed-stage-actions";

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
  // The kid/adult wording is contract copy, so the raw band token never reaches a screen.
  function bandLabel(band: string): string {
    if (band === "kid") return copy(pageContract, "action.retag.band_kid");
    if (band === "adult") return copy(pageContract, "action.retag.band_adult");
    return "";
  }

  const choices: InlineChoice[] = stages.map((stage) => ({
    value: stage.code,
    label: stage.label,
    description: stage.description,
    // The band an animal INHERITS from this tag, shown in the list because a cohort name does not
    // otherwise announce that it moves kids to adults.
    hint: bandLabel(stage.band),
  }));

  const slice = { shed_id: shedId, ...(partitionLabel ? { partition_label: partitionLabel } : {}) };

  return (
    <InlineCellEditor
      pageContract={pageContract}
      current={currentTag}
      emptyLabel={emptyLabel}
      choices={choices}
      enabled={enabled}
      disabledReason={disabledReason}
      renderCurrent={(value) => <span className="tag">{value}</span>}
      onPreview={async (value) => {
        const result = await previewShedStageAction({ ...slice, management_stage: value });
        if (!result.ok) return { error: result.error.message || copy(pageContract, "action.retag.failed") };
        const preview = result.data;
        // The band is stated only when it actually MOVES the animals: repeating the band they
        // already carry is noise. The buckets are whole-pen and disjoint, so one differing bucket
        // is enough to know it moves.
        const bandMoves = (preview.current_stages ?? []).some((bucket) => bucket.age_band !== preview.age_band);
        const consequence =
          preview.age_band && preview.total_live > 0 && bandMoves
            ? `${copy(pageContract, "action.retag.becomes")} ${bandLabel(preview.age_band)}`
            : undefined;
        return {
          subject: preview.operational_location_display,
          count: preview.total_live,
          consequence,
        } satisfies InlinePreview;
      }}
      onApply={async (value, reason, idempotencyKey) => {
        const result = await commitShedStageAction(
          {
            ...slice,
            management_stage: value,
            reason,
            // An empty pen is a legitimate thing to configure from this screen, so the write is
            // told to accept one. A caller meaning "retag these animals" leaves it off and still
            // gets the wrong-pen refusal.
            configure_empty: true,
          },
          idempotencyKey,
        );
        return result.ok ? {} : { error: result.error.message || copy(pageContract, "action.retag.failed") };
      }}
    />
  );
}
