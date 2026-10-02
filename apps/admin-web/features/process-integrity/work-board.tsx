import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import Box from "@mui/material/Box";
import type { ActionCenterObligation, ProcessIntegritySeverity, WorkState } from "@/lib/api/server";
import { copy, optionGroup, optionalOption, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { toneColor, type Tone } from "./process-integrity";
import { fmtDate } from "@/lib/format";
import { operationalLocationLabel } from "@/lib/operational-location.ts";
import { actionDriveLabel, actionWorkTitle } from "./action-center-presenters";
import { stageLabel } from "@/lib/stage-labels";
import { stageVocabularyLabel, type StageNameMap } from "@/lib/stage-display";
import { KanbanBoard, KanbanColumn } from "@/components/app/kanban";
import { ItemContent, ItemInfo, ItemName, ItemStatus, type ItemStatusProps } from "@/components/app/kanban/item-styles";
import { Label } from "@/components/minimal/label";
import { ActionCenterCardShell } from "./action-center-board-parts";
import { TAP_MIN } from "@/theme/tap-target";

export { actionDriveLabel, actionWorkTitle } from "./action-center-presenters";

// Template kanban item anatomy (sections/kanban/item): priority arrow, one name line, caption lines,
// then the info row. TR1-#26: at most TWO Labels per card (the work state, plus the drive-capacity
// state when there is one); every other fact is a caption line, never another chip.
// guard: action-center-card-anatomy
export const MAX_CARD_LABELS = 2;

// The template priority arrow reads the server severity: broken is high, at risk medium, watch low.
function severityStatus(severity: ProcessIntegritySeverity): ItemStatusProps["status"] {
  if (severity === "broken") return "high";
  if (severity === "at_risk") return "medium";
  if (severity === "watch") return "low";
  return null;
}

function initials(name?: string): string {
  if (!name) return "!";
  return name
    .split(/\s+/)
    .map((w) => w[0] ?? "")
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

function driveCapacityTag(pageContract: AdminUiPageContract, row: ActionCenterObligation): { tone: Tone; label: string; title: string } | null {
  switch (row.drive_capacity_state) {
    case "over_cap_required": {
      const slots = (row.drive_available_operators ?? 0) * (row.drive_operator_cap ?? 0);
      const animals = row.drive_animals_assigned ?? row.drive_animals_required ?? 0;
      return {
        tone: "dng",
        label: copy(pageContract, "label.drive_over_cap_required"),
        title: copy(pageContract, "tooltip.drive_over_cap_required")
          .replace("{animals}", String(animals))
          .replace("{slots}", String(slots)),
      };
    }
    case "medical_defer":
      return {
        tone: "warn",
        label: copy(pageContract, "label.drive_medical_defer"),
        title: row.drive_medical_defer_reason ?? copy(pageContract, "tooltip.drive_medical_defer"),
      };
    case "terminal_animal_closed":
      return {
        tone: "mut",
        label: copy(pageContract, "label.drive_terminal_closed"),
        title: row.drive_medical_defer_reason ?? copy(pageContract, "tooltip.drive_terminal_closed"),
      };
    default:
      return null;
  }
}

function shortDueLabel(value: string): string {
  return fmtDate(value);
}

function optionLabel(options: AdminUiOption[], key: string): string {
  const option = options.find((item) => item.key === key);
  if (!option) throw new Error(`Admin-web contract missing option ${key}`);
  return option.label;
}

function optionTone(options: AdminUiOption[], key: string): Tone {
  const option = options.find((item) => item.key === key);
  if (!option) throw new Error(`Admin-web contract missing option ${key}`);
  return (option.tone || "mut") as Tone;
}

/**
 * A stage code in the tenant's own words (A3, pr294: cards read "K2"): the stage vocabulary's name
 * when it has one, else the code with the fattening token worded. Presentation only.
 */
export function stageWords(code: string | null | undefined, names?: StageNameMap): string {
  if (!code) return "";
  const named = names ? stageVocabularyLabel(code, names) : code;
  return named === code ? stageLabel(code) : named;
}

// Five lanes (A1, pr294; L-N4): squeezing all five into the 1,060px content column made each lane
// ~190px -- "Skipped — sil…" cut, card titles four lines deep. Each lane keeps a readable 260px and
// the board row pans sideways inside its own scroller (KanbanBoard overflow-x from sm), so every
// lane stays reachable. A phone still stacks / swipes one lane at a time.
export const FIVE_LANE_MIN_WIDTH = "calc(32.5 * var(--spacing))"; // 260px
const FIVE_LANE_BOARD_SX = {
  "--kanban-column-width": {
    xs: "86vw",
    md: FIVE_LANE_MIN_WIDTH,
  },
} as const;

const CAPTION_SX = { display: "block", mt: 0.5, typography: "caption", color: "text.secondary", overflowWrap: "anywhere" } as const;

// One template kanban card for a single Action Center obligation. Same facts as before (drive, pen,
// park, due date, stage, severity, work / proof / capacity state, progress, blocker, owner); only the
// anatomy changed: they read as caption lines under the name instead of five chips.
function WorkCard({ pageContract, row, href, localOverlay, stageNames }: { pageContract: AdminUiPageContract; row: ActionCenterObligation; href: string; localOverlay: boolean; stageNames?: StageNameMap }) {
  const operatorMissing = row.owner_state === "missing" || !row.owner?.operator_name;
  const blocker = row.blocker_reason || null;
  const drive = actionDriveLabel(pageContract, row);
  const title = actionWorkTitle(pageContract, row);
  const ownerLabel = operatorMissing ? copy(pageContract, "label.owner_chain_assign") : (row.owner?.operator_name ?? "");
  const progress = row.expected_count > 0 ? `${row.completed_count}/${row.expected_count} ${copy(pageContract, "label.done_suffix")}` : null;
  const showBlocker = blocker && !operatorMissing;
  const shedDisplay = row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name, partitionLabel: row.partition_label });
  const openLabel = `${copy(pageContract, "label.open_work_item_for")} ${shedDisplay || copy(pageContract, "label.shed_fallback")}`;
  const parkDisplay = optionalOption(pageContract, "park_display_chips", row.park_id);
  const parkLabel = parkDisplay?.label || row.park_name || row.park_id;
  const severityOptions = optionGroup(pageContract, "severity_chips");
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const proofStateOptions = optionGroup(pageContract, "proof_state_chips");
  const severityLabel = optionLabel(severityOptions, row.severity);
  const driveTag = driveCapacityTag(pageContract, row);
  const proofLabel = row.proof_state !== "missing" ? optionLabel(proofStateOptions, row.proof_state) : null;
  const labels = [
    { key: "state", color: toneColor(optionTone(workStateOptions, row.work_state)), text: optionLabel(workStateOptions, row.work_state), title: undefined as string | undefined },
    ...(driveTag ? [{ key: "drive", color: toneColor(driveTag.tone), text: driveTag.label, title: driveTag.title }] : []),
  ].slice(0, MAX_CARD_LABELS);
  const dueLine = `${copy(pageContract, "label.due_prefix")} ${shortDueLabel(row.due_at)}`;
  const facts = [
    [shedDisplay || copy(pageContract, "label.vaccination"), parkLabel].filter(Boolean).join(" · "),
    [dueLine, stageWords(row.animal_stage, stageNames), severityLabel].filter(Boolean).join(" · "),
    proofLabel,
    row.drive_capacity_state === "over_cap_required"
      ? `${(row.drive_animals_assigned ?? row.drive_animals_required ?? 0).toLocaleString("en-IN")} animals · ${(row.drive_available_operators ?? 0).toLocaleString("en-IN")} ops × ${(row.drive_operator_cap ?? 0).toLocaleString("en-IN")}`
      : null,
  ].filter((line): line is string => Boolean(line));
  const contents = (
    <ItemContent>
      <ItemStatus status={severityStatus(row.severity)} aria-label={severityLabel} />
      <ItemName name={title} sx={{ whiteSpace: "normal", pr: 2.5 }} />
      <Box component="span" sx={CAPTION_SX}>{drive}</Box>
      {facts.map((line, i) => (
        <Box key={i} component="span" sx={CAPTION_SX}>{line}</Box>
      ))}
      {showBlocker ? (
        <Box component="span" sx={{ ...CAPTION_SX, color: "error.main" }}>{blocker.split(" - ")[0]}</Box>
      ) : null}
      <Box sx={{ mt: 1.5, display: "flex", flexWrap: "wrap", gap: 0.75 }}>
        {labels.map((label) => (
          <Label key={label.key} variant="soft" color={label.color} title={label.title}>{label.text}</Label>
        ))}
      </Box>
      <ItemInfo
        assignee={[{ id: row.row_id, name: ownerLabel, initial: initials(row.owner?.operator_name), color: operatorMissing ? "error" : "default" }]}
        assigneeTitle={ownerLabel}
      >
        <Box component="span" sx={{ typography: "caption", color: operatorMissing ? "error.main" : "text.secondary" }}>{ownerLabel}</Box>
        {progress ? <Box component="span" sx={{ typography: "caption", fontWeight: "fontWeightSemiBold", color: "text.secondary" }}>{progress}</Box> : null}
      </ItemInfo>
    </ItemContent>
  );
  const linkProps = {
    href,
    scroll: false,
    "aria-label": openLabel,
    title: `${title} · ${drive} · ${severityLabel} · ${ownerLabel || copy(pageContract, "label.unassigned")}`,
    style: { display: "block", minHeight: TAP_MIN, color: "inherit", textDecoration: "none", borderRadius: "inherit" },
  } as const;
  return (
    <ActionCenterCardShell>
      {localOverlay ? <LocalOverlayLink {...linkProps}>{contents}</LocalOverlayLink> : <Link {...linkProps}>{contents}</Link>}
    </ActionCenterCardShell>
  );
}

type BoardColumn = {
  key: string;
  label: string;
  tone: Tone;
  states: WorkState[];
};

// The mock Action Center is a six-lane status board. Vaccination has richer backend work states, so the
// visual lanes stay mock-shaped while each card still carries the exact server-computed work state.
const BOARD_COLUMN_STATES: Record<string, WorkState[]> = {
  pending: ["due", "scheduled", "proof_pending", "verification_pending", "in_progress"],
  ontime: ["completed"],
  late: ["overdue", "missed"],
  skipped: ["deferred"],
  deviated: ["blocked", "rejected"],
};

function boardColumns(pageContract: AdminUiPageContract): BoardColumn[] {
  return optionGroup(pageContract, "work_state_board_columns").map((option) => ({
    key: option.key,
    label: option.label,
    tone: (option.tone || "mut") as Tone,
    states: BOARD_COLUMN_STATES[option.key] ?? [],
  }));
}

export function boardWorkStates(pageContract: AdminUiPageContract): WorkState[] {
  const states: WorkState[] = [];
  for (const column of boardColumns(pageContract)) {
    for (const state of column.states) {
      if (!states.includes(state)) states.push(state);
    }
  }
  return states;
}

function boardColumnFor(row: ActionCenterObligation, columns: BoardColumn[]): BoardColumn {
  return columns.find((column) => column.states.includes(row.work_state)) ?? columns[0];
}

function columnCount(column: BoardColumn, stateCounts?: ReadonlyMap<WorkState, number>): number | undefined {
  if (!stateCounts) return undefined;
  return column.states.reduce((sum, state) => sum + (stateCounts.get(state) ?? 0), 0);
}

// Template kanban status board (components/app/kanban over sections/kanban): one column per visual
// lane (count Label + name), template cards inside, an empty column stays an empty column (no
// placeholder box). Source rows are the real ActionCenterResponse items — server-computed work state,
// shown inside each card.
export function WorkBoard({
  pageContract,
  rows,
  stateCounts,
  showAllColumns = true,
  drawerHrefForRow,
  stageNames,
}: {
  pageContract: AdminUiPageContract;
  rows: ActionCenterObligation[];
  stateCounts?: ReadonlyMap<WorkState, number>;
  showAllColumns?: boolean;
  drawerHrefForRow?: (row: ActionCenterObligation) => string;
  /** The tenant stage vocabulary, so a card names the stage rather than its code. */
  stageNames?: StageNameMap;
}) {
  const contractColumns = boardColumns(pageContract);
  const byColumn = new Map<string, ActionCenterObligation[]>();
  for (const r of rows) {
    const column = boardColumnFor(r, contractColumns);
    const list = byColumn.get(column.key) ?? [];
    list.push(r);
    byColumn.set(column.key, list);
  }
  const columns = showAllColumns ? contractColumns : contractColumns.filter((column) => (byColumn.get(column.key)?.length ?? 0) > 0);

  return (
    <KanbanBoard data-ac-board role="group" aria-label={copy(pageContract, "section.work_board.aria")} tabIndex={0} sx={columns.length > 4 ? FIVE_LANE_BOARD_SX : undefined}>
      {columns.map((column) => {
        const col = byColumn.get(column.key) ?? [];
        const count = columnCount(column, stateCounts) ?? col.length;
        return (
          <KanbanColumn key={column.key} title={column.label} count={count}>
            {col.map((row) => (
              <WorkCard
                key={row.row_id}
                pageContract={pageContract}
                row={row}
                href={drawerHrefForRow ? drawerHrefForRow(row) : `/workflows/${encodeURIComponent(row.row_id)}`}
                localOverlay={Boolean(drawerHrefForRow)}
                stageNames={stageNames}
              />
            ))}
          </KanbanColumn>
        );
      })}
    </KanbanBoard>
  );
}
