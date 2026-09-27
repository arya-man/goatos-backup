"use client";

import type { ReactNode } from "react";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Stack from "@mui/material/Stack";
import TableCell, { type TableCellProps } from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";

import { InfoTip } from "@/components/app/info-tip";
import { PagedRows } from "@/components/app/paged-rows";
import { Label, type LabelColor } from "@/components/minimal/label";

// /vaccination command-board sections as template table cards (TR1-#21, guard:
// vaccination-template-anatomy). Every matrix is a Card + CardHeader (title, info tip) over a
// template table that scrolls sideways inside the template Scrollbar and pages its rows with the
// template pager. State colour is the template Label (soft palette tint), never a painted cell.

/** Rows per page on the matrix cards: short enough that the page reads like a dashboard. */
export const MATRIX_ROWS_PER_PAGE = 10;

export function MatrixCard({
  id,
  title,
  info,
  subheader,
  legend,
  tabs,
  head,
  rows,
  ariaLabel,
  minWidth,
  empty,
}: {
  id?: string;
  title: string;
  info?: string;
  subheader?: ReactNode;
  legend?: ReactNode;
  tabs?: ReactNode;
  head: ReactNode;
  rows: ReactNode[];
  ariaLabel: string;
  minWidth?: number;
  empty?: ReactNode;
}) {
  return (
    <Card id={id} sx={{ minWidth: 0 }}>
      <CardHeader title={title} subheader={subheader} action={info ? <InfoTip title={info} /> : undefined} sx={{ mb: legend ? 2 : 3 }} />
      {legend ? <Box sx={{ px: 3, pb: 2 }}>{legend}</Box> : null}
      {tabs}
      <PagedRows
        rows={rows}
        head={head}
        wrapClassName=""
        scrollbar
        tableMinWidth={minWidth}
        ariaLabel={ariaLabel}
        initialRowsPerPage={MATRIX_ROWS_PER_PAGE}
        empty={empty}
      />
    </Card>
  );
}

/** Legend row: one template Label per state, in the colour the cells use. */
export function StateLegend({ items }: { items: Array<{ key: string; color: LabelColor; label: string }> }) {
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap" }}>
      {items.map((item) => (
        <Label key={item.key} color={item.color}>
          <>{item.label}</>
        </Label>
      ))}
    </Stack>
  );
}

/** First column: the pen / stage name with its qualifier (park, stage note) on a caption line. */
export function RowHeadCell({ primary, secondary }: { primary: ReactNode; secondary?: ReactNode }) {
  return (
    <TableCell component="th" scope="row" sx={{ whiteSpace: "nowrap" }}>
      <Typography variant="subtitle2" component="div">{primary}</Typography>
      {secondary ? (
        <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{secondary}</Typography>
      ) : null}
    </TableCell>
  );
}

/** A matrix cell: a state Label with an optional caption line (dates, counts) under it. */
export function StateCell({
  color,
  value,
  caption,
  selected,
  onActivate,
  ...cellProps
}: {
  color?: LabelColor;
  value: ReactNode;
  caption?: ReactNode;
  selected?: boolean;
  onActivate?: () => void;
} & Omit<TableCellProps, "color" | "onClick" | "onKeyDown">) {
  const actionable = Boolean(onActivate);
  return (
    <TableCell
      {...cellProps}
      role={actionable ? "button" : cellProps.role}
      tabIndex={actionable ? 0 : undefined}
      aria-pressed={actionable && selected !== undefined ? selected : undefined}
      onClick={onActivate}
      onKeyDown={
        onActivate
          ? (event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                onActivate();
              }
            }
          : undefined
      }
      sx={{
        whiteSpace: "nowrap",
        ...(actionable ? { cursor: "pointer", "&:hover": { bgcolor: "action.hover" }, "&:focus-visible": { outlineStyle: "solid", outlineWidth: 2, outlineColor: "primary.main", outlineOffset: -2 } } : {}),
        ...(selected ? { bgcolor: "action.selected" } : {}),
      }}
    >
      {color ? (
        <Label color={color}>
          <>{value}</>
        </Label>
      ) : (
        <Typography variant="body2" component="span" sx={{ color: "text.disabled" }}>{value}</Typography>
      )}
      {caption ? (
        <Typography variant="caption" component="div" sx={{ color: "text.secondary", mt: 0.5 }}>{caption}</Typography>
      ) : null}
    </TableCell>
  );
}

/** A header cell of a matrix (vaccine / dose column). */
export function HeadCell({ children }: { children: ReactNode }) {
  return <TableCell component="th" sx={{ whiteSpace: "nowrap" }}>{children}</TableCell>;
}

/** Empty-table row spanning the whole table. */
export function EmptyRow({ colSpan, children }: { colSpan: number; children: ReactNode }) {
  return (
    <TableRow>
      <TableCell colSpan={colSpan} sx={{ py: 5, textAlign: "center", color: "text.secondary" }}>{children}</TableCell>
    </TableRow>
  );
}
