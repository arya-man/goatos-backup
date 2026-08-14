"use client";

// Counts Breakdown -> the Breed and Gender cells: correct one ROW.
//
// SCOPE IS THE ROW, NOT THE PEN, which is the whole difference from the Stage cell next to it. A
// cohort tag is a property of the PEN, so retagging moves everything standing in it. Breed and sex
// are properties of the ANIMAL, and one pen legitimately holds several of each -- CBE Gandhi - 3
// holds Beetal, Sojat and Sirohi females together -- so this touches only the animals matching the
// exact slice the operator is looking at.
//
// It is a DATA CORRECTION, not a husbandry event: nothing about the animal changed, only what the
// register says about it. Sex especially is a biological fact rather than a setting, so the reason
// carried into the audit row is the account of why the record was changed.
//
// It does NOT re-evaluate anything downstream that keyed on the old value -- a vaccination schedule
// derived from a wrong sex is not recomputed by this write.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { commitCensusCorrectionAction, previewCensusCorrectionAction } from "./census-correction-actions";
import { InlineCellEditor, type InlineChoice, type InlinePreview } from "./inline-cell-editor";

// CensusSlice is the row's identity: every field participates in the write's predicate, so dropping
// one would widen the correction to animals the operator never saw.
export type CensusSlice = {
  shedId: string;
  partitionLabel: string;
  managementStage: string;
  breed: string;
  // The generated contract narrows this to the column's own domain. A census row can only carry one
  // of the two, so the narrowing is honest -- and a row that somehow carried anything else must not
  // be silently corrected against a slice the backend would read differently.
  sex: "female" | "male";
};

export function CensusValueEditor({
  pageContract,
  slice,
  field,
  current,
  emptyLabel,
  choices,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  slice: CensusSlice;
  field: "breed" | "sex";
  current: string;
  emptyLabel: string;
  choices: InlineChoice[];
  enabled: boolean;
  disabledReason: string;
}) {
  const body = {
    shed_id: slice.shedId,
    ...(slice.partitionLabel ? { partition_label: slice.partitionLabel } : {}),
    management_stage: slice.managementStage,
    breed: slice.breed,
    sex: slice.sex,
    field,
  };

  return (
    <InlineCellEditor
      pageContract={pageContract}
      current={current}
      emptyLabel={emptyLabel}
      // A value equal to what the row already carries is not offered: the backend rejects it, and
      // offering a no-op only to fail is worse than not offering it.
      choices={choices.filter((choice) => choice.value.toLowerCase() !== current.toLowerCase())}
      enabled={enabled}
      disabledReason={disabledReason}
      onPreview={async (value) => {
        const result = await previewCensusCorrectionAction({ ...body, value });
        if (!result.ok) return { error: result.error.message || copy(pageContract, "action.retag.failed") };
        return {
          subject: result.data.operational_location_display,
          count: result.data.total_live,
        } satisfies InlinePreview;
      }}
      onApply={async (value, reason, idempotencyKey) => {
        const result = await commitCensusCorrectionAction({ ...body, value, reason }, idempotencyKey);
        return result.ok ? {} : { error: result.error.message || copy(pageContract, "action.retag.failed") };
      }}
    />
  );
}
