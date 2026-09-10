import type { Tone } from "@/components/ui-primitives";
import type { AdminUiOption, AdminUiPageContract } from "@/lib/admin-ui-contract";
import { optionGroup } from "@/lib/admin-ui-contract";
import type { WorkBoardLane, WorkBoardModule, WorkBoardRow, WorkBoardWorkState } from "@/lib/api/work-board-server";

export const WORK_BOARD_PATH = "/work-board";

// The board's URL parameters. Park rides the shared top-bar scope (`park`); the rest are the page's own.
export const PARAM_DATE = "date";
export const PARAM_MODULE = "module";
export const PARAM_STATE = "state";
export const PARAM_OWNER = "owner";
export const PARAM_LIMIT = "limit";
export const PARAM_CURSOR = "cursor";
export const PARAM_ROW = "row";

const TONES: Tone[] = ["ok", "warn", "dng", "info", "mut", "pur", "teal"];

export function toneOf(option: AdminUiOption | undefined, fallback: Tone = "mut"): Tone {
  const tone = option?.tone as Tone | undefined;
  return tone && TONES.includes(tone) ? tone : fallback;
}

export function findOption(options: AdminUiOption[], key: string): AdminUiOption | undefined {
  return options.find((option) => option.key === key);
}

export type LaneMeta = { key: WorkBoardLane; label: string; tone: Tone; title: string };

export function lanes(pageContract: AdminUiPageContract): LaneMeta[] {
  return optionGroup(pageContract, "work_board_lanes").map((option) => ({
    key: option.key as WorkBoardLane,
    label: option.label,
    tone: toneOf(option),
    title: option.title ?? "",
  }));
}

export function moduleOptions(pageContract: AdminUiPageContract): AdminUiOption[] {
  return optionGroup(pageContract, "work_board_modules");
}

export function stateOptions(pageContract: AdminUiPageContract): AdminUiOption[] {
  return optionGroup(pageContract, "work_board_states");
}

export function parkOptions(pageContract: AdminUiPageContract): AdminUiOption[] {
  return optionGroup(pageContract, "work_board_parks");
}

// Tint for the clock chip, from the SERVER-computed state and severity — no client date math.
export function clockClass(row: WorkBoardRow): string {
  if (row.severity === "broken" || row.severity === "at_risk" || row.work_state === "overdue" || row.work_state === "missed") return "brk";
  if (row.severity === "watch" || row.work_state === "rejected" || row.work_state === "blocked") return "run";
  if (row.work_state === "completed") return "ok";
  return "";
}

export function needsAttention(row: WorkBoardRow): boolean {
  return row.counts.needs_attention > 0 || row.severity === "at_risk" || row.severity === "broken";
}

export function initials(name?: string): string {
  if (!name) return "—";
  return name
    .split(/\s+/)
    .map((word) => word[0] ?? "")
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

export type OwnerOption = { id: string; name: string };

// Distinct owners among the rows on screen, for the assignee picker. The picker narrows the
// BACKEND read (owner=<user id>), so results are server truth; only the option list is
// page-local, which is why it carries no counts.
export function ownersOnPage(rows: WorkBoardRow[]): OwnerOption[] {
  const seen = new Map<string, OwnerOption>();
  for (const row of rows) {
    const id = row.owner?.user_id;
    if (!id || seen.has(id)) continue;
    seen.set(id, { id, name: row.owner?.name || id.slice(0, 8) });
  }
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
}

export function modulesVisible(all: AdminUiOption[], visible: WorkBoardModule[]): AdminUiOption[] {
  const keep = new Set<string>(visible);
  return all.filter((option) => keep.has(option.key));
}

export function isWorkState(value: string, options: AdminUiOption[]): value is WorkBoardWorkState {
  return options.some((option) => option.key === value);
}
