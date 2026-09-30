"use client";

import { ListRowsSkeleton } from "@/components/app/skeletons";
import { visuallyHidden } from "@mui/utils";
import Link from "@/components/no-prefetch-link";
import { useEffect, useState, useTransition } from "react";
import Box from "@mui/material/Box";
import List from "@mui/material/List";
import Avatar from "@mui/material/Avatar";
import Button from "@mui/material/Button";
import Collapse from "@mui/material/Collapse";
import ListItem from "@mui/material/ListItem";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import ListItemText from "@mui/material/ListItemText";
import ListItemButton from "@mui/material/ListItemButton";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow, WorkBoardSubtask, WorkBoardSubtaskPage } from "@/lib/api/work-board-server";
import { loadSubtasksAction } from "./actions";
import { initials, lanes } from "./work-board-model";

// The issue view's subtask list (template kanban details "Subtasks" tab: the "N of M" line, then
// the list): worst first, ten per page, each row with its name, subtitle, owner and ONE status
// Label; a click unfolds the steps. The page is read through a Server Action only while the card
// is open; nothing here recomputes state, every state and label is the backend's.
const PAGE = 10;

function stepLabel(pageContract: AdminUiPageContract, state: string): string {
  return copy(pageContract, `step.${state}`, state);
}

type LabelColor = "default" | "success" | "info" | "warning" | "error";

function stepColor(state: string): LabelColor {
  return state === "done" ? "success" : state === "in_review" || state === "in_progress" ? "info" : state === "rework" ? "warning" : state === "needs_attention" ? "error" : "default";
}

function SubtaskRow({ pageContract, sub }: { pageContract: AdminUiPageContract; sub: WorkBoardSubtask }) {
  const [open, setOpen] = useState(false);
  const laneOpt = lanes(pageContract).find((lane) => lane.key === sub.lane);
  const status = sub.needs_attention ? copy(pageContract, "tile.attention") : laneOpt?.label ?? sub.lane;
  const color: LabelColor = sub.needs_attention ? "warning" : sub.lane === "done" ? "success" : sub.lane === "in_review" || sub.lane === "in_progress" ? "info" : "default";
  return (
    <>
      <ListItem
        disablePadding
        secondaryAction={
          sub.href ? (
            <IconButton component={Link} href={sub.href} title={copy(pageContract, "drawer.subtasks.open")} aria-label={copy(pageContract, "drawer.subtasks.open")}>
              <Iconify icon="eva:external-link-fill" />
            </IconButton>
          ) : undefined
        }
      >
        <ListItemButton aria-expanded={open} onClick={() => setOpen((v) => !v)} sx={{ gap: 1.5, pr: sub.href ? 7 : 2, borderRadius: 0.75 }}>
          <Iconify icon={open ? "eva:arrow-ios-downward-fill" : "eva:arrow-ios-forward-fill"} width={16} sx={{ color: "text.disabled", flexShrink: 0 }} />
          <ListItemText
            primary={sub.name}
            secondary={
              <>
                {sub.subtitle || null}
                {/* Step labels live in the expanded panel below; the collapsed row is name · owner · ONE status. */}
                <Box component="span" sx={visuallyHidden}>
                  {sub.steps.map((step) => `${step.name} ${stepLabel(pageContract, step.state)}`).join(", ")}
                </Box>
              </>
            }
            slotProps={{ primary: { variant: "subtitle2", noWrap: true }, secondary: { variant: "caption", noWrap: true } }}
            sx={{ minWidth: 0, m: 0 }}
          />
          {sub.owner?.name ? (
            <Avatar title={sub.owner.name} sx={{ width: "calc(3 * var(--spacing))", height: "calc(3 * var(--spacing))", typography: "caption", flexShrink: 0 }}>
              {initials(sub.owner.name)}
            </Avatar>
          ) : null}
          <Label variant="soft" color={color} sx={{ flexShrink: 0 }}>{status}</Label>
        </ListItemButton>
      </ListItem>
      <Collapse in={open} unmountOnExit>
        <List disablePadding sx={{ pl: 5, pb: 1 }}>
          {sub.steps.map((step, i) => (
            <ListItem key={`${step.name}-${i}`} sx={{ gap: 1.5, py: 0.75 }}>
              <Avatar sx={{ width: "calc(2.5 * var(--spacing))", height: "calc(2.5 * var(--spacing))", typography: "caption", bgcolor: "background.neutral", color: "text.secondary" }}>{i + 1}</Avatar>
              <ListItemText
                primary={step.name}
                secondary={step.detail || null}
                slotProps={{ primary: { variant: "body2" }, secondary: { variant: "caption" } }}
                sx={{ minWidth: 0, m: 0 }}
              />
              <Label variant="soft" color={stepColor(step.state)} sx={{ flexShrink: 0 }}>
                {stepLabel(pageContract, step.state)}
              </Label>
            </ListItem>
          ))}
        </List>
      </Collapse>
    </>
  );
}

export function WorkBoardSubtasks({ pageContract, row, selectedOwner }: { pageContract: AdminUiPageContract; row: WorkBoardRow; selectedOwner?: string }) {
  const [pages, setPages] = useState<WorkBoardSubtaskPage[]>([]);
  const [error, setError] = useState(false);
  const [pending, startTransition] = useTransition();
  const load = (cursor: string | undefined, reset: boolean) => {
    startTransition(async () => {
      const result = await loadSubtasksAction({ rowKey: row.row_key, park: row.park_id, businessDate: row.business_date, owner: selectedOwner, cursor });
      if (!result.ok) {
        setError(true);
        return;
      }
      setError(false);
      setPages((prev) => (reset ? [result.page] : [...prev, result.page]));
    });
  };
  useEffect(() => {
    // The parent keys this component by row_key, so a new card is a fresh instance: the first
    // page is read once per open card and state never has to be reset here.
    load(undefined, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const current = pages[pages.length - 1];
  const pageNo = pages.length;
  const total = current?.total ?? 0;
  const first = current ? (pageNo - 1) * PAGE + 1 : 0;
  const last = current ? (pageNo - 1) * PAGE + current.subtasks.length : 0;
  const npages = Math.max(1, Math.ceil(total / PAGE));
  const note = (text: string) => (
    <Typography variant="body2" sx={{ color: "text.secondary" }}>
      {text}
    </Typography>
  );
  if (error) return note(copy(pageContract, "drawer.subtasks.error"));
  if (!current) return note(copy(pageContract, "drawer.subtasks.loading"));
  if (current.subtasks.length === 0) return note(copy(pageContract, "drawer.subtasks.empty"));
  return (
    <Box sx={{ gap: 2, display: "flex", flexDirection: "column" }}>
      <Typography variant="body2">
        {copy(pageContract, "drawer.subtasks.showing")} <b>{first}–{last}</b> {copy(pageContract, "drawer.subtasks.of")} <b>{total}</b> {copy(pageContract, "card.total")} · {copy(pageContract, "drawer.subtasks.worst_first")}
      </Typography>
      {/* Paging swaps the rows to their skeleton (no dimmed old page: "just switch and show shimmer"). */}
      {pending ? (
        <ListRowsSkeleton rows={Math.max(1, current.subtasks.length)} trailing={false} />
      ) : (
        <List disablePadding>
          {current.subtasks.map((sub) => (
            <SubtaskRow key={sub.key} pageContract={pageContract} sub={sub} />
          ))}
        </List>
      )}
      {npages > 1 ? (
        <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 1, typography: "body2" }}>
          <Button size="small" color="inherit" disabled={pageNo <= 1 || pending} onClick={() => setPages((prev) => prev.slice(0, -1))} startIcon={<Iconify icon="eva:arrow-ios-back-fill" width={16} />}>
            {copy(pageContract, "action.previous")}
          </Button>
          <span>
            {copy(pageContract, "drawer.subtasks.page")} <b>{pageNo}</b> {copy(pageContract, "drawer.subtasks.of")} <b>{npages}</b>
          </span>
          <Button size="small" color="inherit" disabled={!current.next_cursor || pending} onClick={() => load(current.next_cursor, false)} endIcon={<Iconify icon="eva:arrow-ios-forward-fill" width={16} />}>
            {copy(pageContract, "action.next")}
          </Button>
        </Box>
      ) : null}
    </Box>
  );
}
