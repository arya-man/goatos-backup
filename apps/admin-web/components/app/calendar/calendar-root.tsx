"use client";

import type { CSSObject } from "@mui/material/styles";

import { styled } from "@mui/material/styles";

import { CalendarRoot as TemplateCalendarRoot } from "@/components/minimal/calendar/styles";

// The verbatim template CalendarRoot (components/minimal/calendar/styles.tsx) plus the admin-web
// shell fixes, layered on top with styled():
// The page shell's legacy table rules (`.wrap/.screen/.main th|td`: paper-2 fill, 48-57px heights,
// 16-24px padding, sticky thead, dashed / removed bottom borders, corner radii) also match
// FullCalendar's structural cells: that drew the tall grey weekday band and inset the weekend
// shading. `.fc.fc th|td` (0,3,1) outranks them and puts back FullCalendar's own values for exactly
// those properties; FullCalendar's more specific cell rules that the reset would otherwise beat
// are re-stated after it (0,4,x).
const legacyTableReset: CSSObject = {
  "& .fc.fc th, & .fc.fc td": {
    padding: 0,
    height: "auto",
    lineHeight: "normal",
    background: "transparent",
    position: "static",
    borderRadius: 0,
    borderBottom: "1px solid var(--fc-border-color, currentColor)",
    textTransform: "none",
    letterSpacing: "normal",
  },
  "& .fc.fc th": { textAlign: "center", verticalAlign: "middle" },
  // The dark shell's `:root:not(.light) .screen th:not(.MuiTableCell-root)` (0,4,1) grey fill
  // outranks the (0,3,1) reset: a third .fc (0,4,1 + later cascade -> 0,5,1) keeps cells clear.
  "& .fc.fc.fc th, & .fc.fc.fc td": { background: "transparent" },
  "& .fc.fc .fc-scrollgrid-section > td": { height: "1px" },
  "& .fc.fc .fc-scrollgrid-section-liquid > td": { height: "100%" },
  "& .fc.fc .fc-scrollgrid-section-header > *, & .fc.fc .fc-scrollgrid-section-footer > *": { borderBottomWidth: 0 },
  "& .fc.fc .fc-timegrid-slot": { height: "1.5em", borderBottom: 0 },
};

export const CalendarRoot = styled(TemplateCalendarRoot)(({ theme }) => {
  // The weekday header: `.fc.fc.fc thead th.fc-col-header-cell` (0,5,2) outranks the shell's
  // `.wrap thead th` and dark `.screen th` fills and the reset, so the header stays one thin template row.
  const tableHeadStyles: CSSObject = {
    "& .fc.fc.fc thead th.fc-col-header-cell": {
      padding: 0,
      background: "transparent",
      textTransform: "none",
      letterSpacing: "normal",
      color: theme.vars.palette.text.primary,
      "&.fc-day-sat, &.fc-day-sun": { color: theme.vars.palette.text.secondary },
      fontSize: "inherit",
      fontWeight: "inherit",
      position: "static",
    },
  };
  // Events open the detail drawer, so they take the pointer.
  const clickable: CSSObject = { "& .fc-event, & .fc-list-event": { cursor: "pointer" } };
  const listCell: CSSObject = { "& .fc.fc .fc-list-table td": { padding: theme.spacing(1, 1.75) } };
  return { ...legacyTableReset, ...tableHeadStyles, ...listCell, ...clickable };
});
