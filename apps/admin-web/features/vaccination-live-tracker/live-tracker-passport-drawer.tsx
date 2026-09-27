"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { useEffect, useRef, useState } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import { LinkButton } from "@/components/app/link-button";
import { DetailDrawer, DrawerBlock, DrawerMetaGrid, DrawerMetaItem, DrawerTableScroll } from "@/components/app/detail-drawer";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { VaccinationPassport } from "@/lib/api/server";
import type { LiveTrackerComboRow } from "@/lib/api/vaccination-live-tracker";
import { fmtDate } from "@/lib/format";

// Goat Passport drawer for the combo-dose rows. Every admin-web table that lists individual animals
// opens this same local overlay rather than navigating away, so the board keeps its live state while
// the animal is inspected.

function rowId(row: LiveTrackerComboRow): string {
  return row.goat_id;
}

async function readVaccinationPassport(goatId: string): Promise<{ ok: true; data: VaccinationPassport } | { ok: false; error: string }> {
  try {
    const response = await fetch(`/api/goats/${encodeURIComponent(goatId)}/vaccination-passport`, {
      headers: { Accept: "application/json" },
      cache: "no-store",
    });
    const payload = (await response.json().catch(() => ({}))) as Partial<VaccinationPassport> & { error?: string };
    if (!response.ok || !payload.goat_id) {
      return { ok: false, error: payload.error ?? `vaccination_passport_read_${response.status}` };
    }
    return { ok: true, data: payload as VaccinationPassport };
  } catch {
    return { ok: false, error: "vaccination_passport_unreachable" };
  }
}

function obligationTone(status: string): "ok" | "warn" | "dng" | "info" | "mut" {
  if (status === "deferred" || status === "missed") return "warn";
  if (status === "due" || status === "in_progress") return "info";
  return "mut";
}

const DRAWER_ROW_LIMIT = 6;

export function LiveTrackerPassportDrawer({
  rows,
  initialSelectedGoatId,
  closeHref,
  pageContract,
}: {
  rows: LiveTrackerComboRow[];
  initialSelectedGoatId?: string;
  closeHref: string;
  pageContract: AdminUiPageContract;
}) {
  const { displayedItem, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: rows,
    itemId: rowId,
    selectionKey: "goat_passport",
    initialSelectedId: initialSelectedGoatId,
    closeHref,
  });
  const [passports, setPassports] = useState<Record<string, VaccinationPassport>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const requestsRef = useRef(new Map<string, ReturnType<typeof readVaccinationPassport>>());

  useEffect(() => {
    if (!displayedItem || passports[displayedItem.goat_id] || errors[displayedItem.goat_id]) return;
    const goatId = displayedItem.goat_id;
    let active = true;
    const request = requestsRef.current.get(goatId) ?? readVaccinationPassport(goatId);
    requestsRef.current.set(goatId, request);
    void request.then((result) => {
      if (!active) return;
      if (result.ok) setPassports((current) => ({ ...current, [goatId]: result.data }));
      else setErrors((current) => ({ ...current, [goatId]: result.error }));
    });
    return () => {
      active = false;
    };
  }, [displayedItem, errors, passports]);

  if (!displayedItem) return null;
  const passport = passports[displayedItem.goat_id];
  const error = errors[displayedItem.goat_id];
  const open = passport?.open_obligations ?? [];
  const placeholder = copy(pageContract, "label.placeholder");

  return (
    <DetailDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={copy(pageContract, "drawer.passport.aria")}
      eyebrow={displayedItem.display_id || displayedItem.primary_tag}
      ariaLabel={copy(pageContract, "drawer.passport.aria")}
      closeLabel={copy(pageContract, "drawer.passport.close_label")}
      footer={
        <>
          <LinkButton href={`/goats/${encodeURIComponent(displayedItem.goat_id)}`} variant="contained">
            {copy(pageContract, "drawer.passport.full_history")}
          </LinkButton>
          <Button variant="outlined" color="inherit" onClick={closeDrawer}>
            {copy(pageContract, "action.close")}
          </Button>
        </>
      }
    >
      {/* Record anatomy: the two-column meta grid, never a flat help grid. */}
      <DrawerMetaGrid>
        <DrawerMetaItem label={copy(pageContract, "drawer.passport.tag_1")}>{displayedItem.primary_tag || placeholder}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.passport.tag_2")}>{displayedItem.secondary_tag || placeholder}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.passport.shed")}>{displayedItem.shed_label || placeholder}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.passport.next_due")}>
          {passport?.next_due
            ? fmtDate(passport.next_due.scheduled_for || passport.next_due.due_at)
            : copy(pageContract, "drawer.passport.no_upcoming")}
        </DrawerMetaItem>
      </DrawerMetaGrid>

      {error ? (
        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          {copy(pageContract, "drawer.passport.unavailable_prefix")}: {error}
        </Typography>
      ) : !passport ? (
        <Typography variant="body2" sx={{ color: "text.secondary" }} aria-live="polite">
          {copy(pageContract, "state.loading")}
        </Typography>
      ) : (
        <DrawerBlock title={copy(pageContract, "drawer.passport.open_obligations")}>
          {open.length === 0 ? (
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "drawer.passport.empty_open")}
            </Typography>
          ) : (
            <Box tabIndex={0} role="group" aria-label={copy(pageContract, "drawer.passport.open_obligations")}>
              <DrawerTableScroll>
                <Table size="small" sx={{ minWidth: 360 }}>
                  {/* Three unheadered columns is a table the reader has to decode. The sibling
                      passport drawers in counts and vaccination-sheds both label theirs. */}
                  <TableHead>
                    <TableRow>
                      <TableCell component="th">{copy(pageContract, "drawer.passport.col_due")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "drawer.passport.col_dose")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "drawer.passport.col_status")}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {open.slice(0, DRAWER_ROW_LIMIT).map((due) => (
                      <TableRow key={due.obligation_id}>
                        <TableCell>{fmtDate(due.scheduled_for || due.due_at)}</TableCell>
                        <TableCell>{due.display_label}</TableCell>
                        <TableCell>
                          <Tag tone={obligationTone(due.status)}>{due.status}</Tag>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </DrawerTableScroll>
              {/* The API returns up to 200 open obligations and this drawer shows 6. Dropping the
                  rest silently is the one thing every sibling drawer in this codebase refuses to
                  do — and combo animals, the only animals this drawer is ever opened on, are by
                  definition the ones most likely to carry more than six. */}
              {open.length > DRAWER_ROW_LIMIT ? (
                <Typography variant="caption" component="p" sx={{ color: "text.secondary", mt: 1 }}>
                  +{open.length - DRAWER_ROW_LIMIT} {copy(pageContract, "drawer.passport.more_suffix")}
                </Typography>
              ) : null}
            </Box>
          )}
        </DrawerBlock>
      )}
    </DetailDrawer>
  );
}
