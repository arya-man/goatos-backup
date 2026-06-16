import type { AdminApiComponents } from "@goatos/api-client";

// UI-safe review-group + bulk-decision metadata shared by the Data Quality page,
// the bulk-review panel, and the review guide. Labels never expose raw upstream
// wording; they describe disagreements as "legacy" data vs the Mesha passport.

export type ReviewGroup = AdminApiComponents["schemas"]["ReviewGroup"];
export type BulkDecisionType = AdminApiComponents["schemas"]["BulkResolveConflictsRequest"]["decision_type"];

const reviewGroupLabels: Record<Exclude<ReviewGroup, "">, string> = {
  legacy_self_conflict: "Legacy self-conflict",
  legacy_sex_mismatch: "Legacy sex mismatch",
  legacy_breed_mismatch: "Legacy breed mismatch",
  legacy_value_mismatch: "Legacy sex & breed mismatch",
  lifecycle_reused_tag: "Lifecycle / reused tag",
};

export function reviewGroupLabel(group: ReviewGroup | undefined): string {
  if (!group) return "Other";
  return reviewGroupLabels[group] ?? "Other";
}

// Groups a reviewer can filter by and bulk-resolve. "" (other) is intentionally
// excluded: those conflicts are not attribute/lifecycle and must be opened
// individually in the workbench.
export const reviewGroupFilterOptions: Exclude<ReviewGroup, "">[] = [
  "legacy_self_conflict",
  "legacy_sex_mismatch",
  "legacy_breed_mismatch",
  "legacy_value_mismatch",
  "lifecycle_reused_tag",
];

export function isReviewGroup(value: string | undefined): value is Exclude<ReviewGroup, ""> {
  return !!value && (reviewGroupFilterOptions as string[]).includes(value);
}

export type BulkDecision = {
  type: BulkDecisionType;
  label: string;
  intent: string;
  appliesTo: Exclude<ReviewGroup, "">[];
  danger?: boolean;
};

// Applicability mirrors the backend (defence in depth; the API is authoritative
// and still rejects an inapplicable batch). keep_passport_value covers every
// legacy attribute group; use_legacy_value excludes self-conflicts (which carry
// more than one legacy value) and is further restricted server-side to approved
// canonical breeds; acknowledge_lifecycle_flag is lifecycle-only.
export const bulkDecisions: BulkDecision[] = [
  {
    type: "keep_passport_value",
    label: "Keep Mesha passport",
    intent: "Keep the Mesha passport value and close the selected conflicts as reviewed. No goat fields change.",
    appliesTo: ["legacy_self_conflict", "legacy_sex_mismatch", "legacy_breed_mismatch", "legacy_value_mismatch"],
  },
  {
    type: "use_legacy_value",
    label: "Use legacy value",
    intent:
      "Overwrite the Mesha passport sex/breed with the single clean legacy value, then close the conflicts. Blocked for self-conflicts and for breeds that are not an approved canonical breed.",
    appliesTo: ["legacy_sex_mismatch", "legacy_breed_mismatch", "legacy_value_mismatch"],
    danger: true,
  },
  {
    type: "acknowledge_lifecycle_flag",
    label: "Acknowledge lifecycle flag",
    intent: "Acknowledge the reused-tag / lifecycle flag and close the selected conflicts. No goat fields change.",
    appliesTo: ["lifecycle_reused_tag"],
  },
];

export const bulkDecisionTypes: BulkDecisionType[] = bulkDecisions.map((decision) => decision.type);

export function decisionAppliesToSelection(decision: BulkDecision, selectedGroups: ReviewGroup[]): boolean {
  if (selectedGroups.length === 0) return false;
  const allowed = new Set<string>(decision.appliesTo);
  return selectedGroups.every((group) => allowed.has(group));
}
