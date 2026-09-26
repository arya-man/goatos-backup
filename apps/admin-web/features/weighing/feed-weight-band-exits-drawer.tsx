"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { useState } from "react";
import { Search } from "lucide-react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import InputAdornment from "@mui/material/InputAdornment";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { DetailDrawer, DrawerBlock, DrawerTableScroll } from "@/components/app/detail-drawer";

import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { BandCell } from "./feed-weight-band-table";
import { EmptyState } from "@/components/app/empty-state";

/** One animal that exited the register inside the period (sold, died or other), already resolved by the card. */
export type FeedWeightBandExitItem = {
  key: string;
  park: string;
  pen: string;
  tag: string;
  gender: string;
  /** The register's stored exit reason / lifecycle text, shown beside the bucket pill. */
  reason: string;
  /** The backend's exit bucket: sold / died / other. */
  bucket: string;
  /** The bucket as display copy. */
  bucketLabel: string;
  /** True when the animal has a weigh inside the period (the last-weigh fields are set). */
  weighed: boolean;
  /** ISO business dates; formatted here. */
  exitedAt: string;
  lastWeighedAt: string;
  lastBand: string;
  lastBandLabel: string;
  lastWeightKg: number | null;
  feedType: string;
  feedTypeLabel: string;
  feedGiven: string;
};

/**
 * One thing the drawer can be opened on: every exit in the period (the chip and the stat tile),
 * or the exits behind one pen × bracket row. The id is what the `#fb_exit=` hash carries.
 */
export type FeedWeightBandExitScope = {
  id: string;
  title: string;
  /** Grouped by pen when the scope spans pens; a single pen scope has one group. */
  items: FeedWeightBandExitItem[];
};

export type FeedWeightBandExitsDrawerLabels = {
  aria: string;
  eyebrow: string;
  /** The exits' bucket and weighed split, shown under the period line. */
  detail: string;
  close: string;
  period: string;
  search: string;
  searchAria: string;
  never: string;
  noFeed: string;
  noGender: string;
  empty: string;
  animals: string;
  animal: string;
  /** Heading of the last group: the animals with no weigh in the period. */
  notWeighed: string;
};

const kg = (value: number) => value.toLocaleString("en-IN", { minimumFractionDigits: 1, maximumFractionDigits: 1 });

function scopeId(scope: FeedWeightBandExitScope): string {
  return scope.id;
}

/**
 * The exited panel: the app's local drawer (the Audit Log record drawer's shape and the same
 * `useLocalOverlaySelection` lifecycle), opened from a `#fb_exit=<scope>` hash so an ordinary
 * click never re-runs the route, Back/Escape/backdrop/X close it, and the table's filters are exactly
 * where the reader left them. The list is grouped by pen and searchable inside the panel.
 */
export function FeedWeightBandExitsDrawer({
  scopes,
  initialSelectedId,
  closeHref,
  contract,
  periodLabel,
  labels,
}: {
  scopes: FeedWeightBandExitScope[];
  initialSelectedId?: string;
  closeHref: string;
  contract: AdminUiTableContract;
  periodLabel: string;
  labels: FeedWeightBandExitsDrawerLabels;
}) {
  const { displayedItem, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: scopes,
    itemId: scopeId,
    selectionKey: "fb_exit",
    initialSelectedId,
    closeHref,
  });
  const [query, setQuery] = useState("");

  if (!displayedItem) return null;
  const q = query.trim().toLowerCase();
  const items = displayedItem.items.filter(
    (item) =>
      q === "" ||
      [item.tag, item.pen, item.gender, item.reason, item.bucketLabel, item.feedGiven, item.park, item.lastBandLabel].join(" ").toLowerCase().includes(q),
  );
  // Weighed animals grouped by pen in served order (newest exit first inside a pen); the
  // animals with no weigh in the period come last, in one group, with their placement as pen.
  const groups: { pen: string; park: string; weighed: boolean; items: FeedWeightBandExitItem[] }[] = [];
  for (const item of items.filter((x) => x.weighed)) {
    const last = groups[groups.length - 1];
    if (last && last.pen === item.pen && last.park === item.park) last.items.push(item);
    else groups.push({ pen: item.pen, park: item.park, weighed: true, items: [item] });
  }
  const notWeighed = items.filter((x) => !x.weighed);
  if (notWeighed.length > 0) groups.push({ pen: "", park: "", weighed: false, items: notWeighed });
  const columns = contract.columns.filter((column) => column.visible && column.key !== "park" && column.key !== "pen");
  const columnsWithPen = contract.columns.filter((column) => column.visible && column.key !== "park");
  const blank = <Box component="span" sx={{ color: "text.disabled" }}>—</Box>;

  // Template temporary drawer (DetailDrawer: portal, backdrop, focus trap + return, 480). The
  // exit tables are wider than the paper, so each scrolls sideways in its own Scrollbar.
  return (
    <DetailDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={
        <>
          {displayedItem.title} · {displayedItem.items.length.toLocaleString("en-IN")}{" "}
          {displayedItem.items.length === 1 ? labels.animal : labels.animals}
        </>
      }
      eyebrow={labels.eyebrow}
      subtitle={
        <>
          {labels.period} {periodLabel}
          {labels.detail ? <> · {labels.detail}</> : null}
        </>
      }
      ariaLabel={labels.aria}
      closeLabel={labels.close}
      footer={
        <Button variant="outlined" color="inherit" onClick={closeDrawer}>
          {labels.close}
        </Button>
      }
    >
      <TextField
        type="search"
        size="small"
        value={query}
        placeholder={labels.search}
        onChange={(event) => setQuery(event.target.value)}
        fullWidth
        slotProps={{
          htmlInput: { "aria-label": labels.searchAria },
          input: {
            startAdornment: (
              <InputAdornment position="start">
                <Search size={18} aria-hidden="true" />
              </InputAdornment>
            ),
          },
        }}
      />
      {groups.length === 0 ? (
        <EmptyState title={labels.empty} />
      ) : (
        groups.map((group) => (
          <DrawerBlock
            key={group.weighed ? `${group.park}|${group.pen}` : "not-weighed"}
            title={
              <>
                {group.weighed ? group.pen : <Box component="span" sx={{ color: "text.secondary" }}>{labels.notWeighed}</Box>}
                <Box component="span" sx={{ color: "text.secondary", typography: "caption" }}>
                  {" "}· {group.weighed ? `${group.park} · ` : ""}{group.items.length.toLocaleString("en-IN")}
                </Box>
              </>
            }
          >
            <DrawerTableScroll>
              <Table
                size="small"
                role="group"
                aria-label={group.weighed ? group.pen : labels.notWeighed}
                sx={{ minWidth: 720, "& td": { whiteSpace: "nowrap" } }}
              >
                <TableHead>
                  <TableRow>
                    {(group.weighed ? columns : columnsWithPen).map((column) => (
                      <TableCell component="th" key={column.key} scope="col" align={column.key === "last_kg" ? "right" : undefined}>
                        {column.label}
                      </TableCell>
                    ))}
                  </TableRow>
                </TableHead>
                <TableBody>
                  {group.items.map((item) => (
                    <TableRow key={item.key}>
                      {(group.weighed ? columns : columnsWithPen).map((column) => (
                        <TableCell key={column.key} align={column.key === "last_kg" ? "right" : undefined}>
                          {column.key === "tag" ? (
                            <b>{item.tag}</b>
                          ) : column.key === "pen" ? (
                            item.pen ? <span title={item.pen}>{item.pen}</span> : blank
                          ) : column.key === "gender" ? (
                            item.gender || <Box component="span" sx={{ color: "text.secondary" }}>{labels.noGender}</Box>
                          ) : column.key === "reason" ? (
                            <span>
                              <Tag tone={item.bucket === "sold" ? "info" : item.bucket === "died" ? "dng" : "warn"}>{item.bucketLabel}</Tag>
                              {item.reason && item.reason !== item.bucket ? (
                                <Box component="span" sx={{ color: "text.secondary", typography: "caption" }}> {item.reason}</Box>
                              ) : null}
                            </span>
                          ) : column.key === "exited_at" ? (
                            fmtDate(item.exitedAt)
                          ) : column.key === "last_weighed" ? (
                            item.lastWeighedAt ? fmtDate(item.lastWeighedAt) : blank
                          ) : column.key === "last_band" ? (
                            item.lastBand ? <BandCell band={item.lastBand} label={item.lastBandLabel} /> : blank
                          ) : column.key === "last_kg" ? (
                            item.lastWeightKg != null ? kg(item.lastWeightKg) : blank
                          ) : column.key === "feed_type" ? (
                            item.feedType ? <Tag tone={item.feedType === "experiment" ? "pur" : "mut"}>{item.feedTypeLabel}</Tag> : blank
                          ) : item.feedGiven ? (
                            <Typography variant="caption" component="span" sx={{ display: "block", whiteSpace: "normal", minWidth: 200, color: "text.secondary" }}>
                              {item.feedGiven}
                            </Typography>
                          ) : (
                            <Box component="span" sx={{ color: "text.secondary" }}>{labels.noFeed}</Box>
                          )}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </DrawerTableScroll>
          </DrawerBlock>
        ))
      )}
    </DetailDrawer>
  );
}
