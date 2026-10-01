// A "Task with its own phone tab" is authored as an SOP on its module's SOP page
// (docs/decisions/simple-task-phone-tabs.md); publishing it writes one routine per park carrying
// that SOP's code. Such a routine is changed ONLY by publishing the SOP again: /routines shows it
// read-only and points at the page that owns it (the backend refuses those writes anyway, 409
// managed_by_sop). Pure module -- no React, no server-only import -- so the node test reads it.

/** The module SOP pages a phone task can be authored on, by SOP code prefix (backend `sopModulePrefixes`). */
export const PHONE_TASK_SOP_PAGES = [
  { prefix: "pc_care.", slice: "pc_care", href: "/pc-care/sops" },
  { prefix: "feed.", slice: "feed", href: "/feed/sops" },
  { prefix: "weighing.", slice: "weighing", href: "/weighing/sops" },
  { prefix: "counts.", slice: "counts", href: "/counts/sops" },
  { prefix: "milk.", slice: "milk", href: "/milk/sops" },
  { prefix: "procurement.", slice: "procurement", href: "/procurement/sops" },
  { prefix: "sales.", slice: "sales", href: "/sales/sops" },
] as const;

export type PhoneTaskSlice = (typeof PHONE_TASK_SOP_PAGES)[number]["slice"];

/** Whether a module SOP page (by its slice) may author a phone task: every module page, never Work instructions or vaccination. */
export function sliceAuthorsPhoneTasks(slice: string): slice is PhoneTaskSlice {
  return PHONE_TASK_SOP_PAGES.some((page) => page.slice === slice);
}

/** The SOP page that owns a routine, or null for a routine authored on /routines (blank `sop_code`). */
export function sopManagedBy(sopCode: string | null | undefined): { slice: PhoneTaskSlice | null; href: string } | null {
  const code = (sopCode ?? "").trim();
  if (!code) return null;
  const page = PHONE_TASK_SOP_PAGES.find((item) => code.startsWith(item.prefix));
  // A code outside every module page still comes from an SOP: it stays read-only, linked nowhere.
  return page ? { slice: page.slice, href: page.href } : { slice: null, href: "" };
}

/** Whether /routines may edit / pause / retire this routine: the caller's own grant, and never an SOP-owned one. */
export function routineEditableHere(routine: { sop_code?: string | null }, granted: boolean): boolean {
  return granted && sopManagedBy(routine.sop_code) === null;
}
