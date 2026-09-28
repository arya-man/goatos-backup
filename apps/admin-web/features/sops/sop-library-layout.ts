import type { PageHeaderLayout } from "@/components/app/page-header";
// Layout constants shared by the SOP library and its loading skeleton, so the skeleton cannot drift.

/** Cards per page: a multiple of the 1/2/3-column grid (template job list), not a table page size. */
export const CARDS_PER_PAGE = 12;
/** Loading placeholder cards: one row of the 3-column grid (module libraries hold 1-4 SOPs). */
export const SOP_SKELETON_CARDS = 3;

/**
 * THE library-vs-editor predicate, shared by the page (module-page.tsx), its URL-panel fallback
 * (`fallbackBy` param list) and the route loading shape (sop-route-skeleton.tsx): the builder opens
 * on `compose=1` or `new=1`; `edit=<id>` only picks WHICH SOP the builder opens.
 */
export const SOP_EDITOR_PARAMS = ["compose", "new"] as const;
export function isSopEditorUrl(params: { get(name: string): string | null | undefined }): boolean {
  return SOP_EDITOR_PARAMS.some((name) => params.get(name) === "1");
}

/** The library header (TR3-P0-3; guard: page-header-layout-twin): below md New SOP (and the weighing
 *  Assumptions button) always take their own row, so the skeleton does not depend on the module's
 *  crumb width or on the reader's permission to edit assumptions. */
export const SOP_HEADER_LAYOUT: PageHeaderLayout = { actionsBelow: true };
/** Header action widths: New SOP; /weighing/sops also mounts Assumptions (for readers with the write). */
export const SOP_HEADER_ACTIONS = { newSop: [108], withAssumptions: [123, 108] } as const;
