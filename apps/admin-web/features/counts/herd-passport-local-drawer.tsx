"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import { LinkButton } from "@/components/app/link-button";
import { DetailDrawer, DrawerBlock, DrawerMetaGrid, DrawerMetaItem, DrawerTableScroll } from "@/components/app/detail-drawer";
import { Caption } from "@/components/app/caption";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { useEffect, useRef, useState } from "react";

import { Tag } from "@/components/ui-primitives";
import { copy, optionalOption, readableOptionKey, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { dash, fmtDate } from "@/lib/format";
import type { VaccinationPassport, VaccinationPassportHistoryItem } from "@/lib/api/server";
import { HerdReproductiveEdit } from "./herd-actions-ui";

/** A status chip's words: the tenant's own label from the page's option group, never the stored key. */
function statusLabel(pageContract: AdminUiPageContract, groupId: string, value: string | null | undefined): string {
  if (!value) return dash(value);
  return optionalOption(pageContract, groupId, value)?.label ?? readableOptionKey(value);
}

type StatusKind = "lifecycle" | "health" | "breeding";
type StatusTone = "ok" | "warn" | "dng" | "info" | "mut";
const DRAWER_ROW_LIMIT = 5;

export type HerdPassportDrawerItem = {
  goatId: string;
  displayId: string;
  tag1?: string | null;
  tag2?: string | null;
  park: string;
  shed: string;
  breed?: string | null;
  sex?: string | null;
  weightKg?: number | null;
  lifecycleStatus?: string | null;
  healthStatus?: string | null;
  reproductiveStatus?: string | null;
};

function statusTone(value: string | null | undefined, kind: StatusKind): StatusTone {
  const normalized = String(value ?? "").toLowerCase();
  if (!normalized) return "mut";
  if (kind === "health") {
    if (["healthy", "normal", "ok"].includes(normalized)) return "ok";
    if (["sick", "critical", "dead"].includes(normalized)) return "dng";
    if (normalized.includes("treatment") || normalized.includes("watch") || normalized.includes("quarantine")) return "warn";
    return "info";
  }
  if (kind === "breeding") {
    if (normalized.includes("pregnant") || normalized.includes("lactating") || normalized.includes("ai")) return "info";
    if (normalized.includes("open") || normalized.includes("none")) return "mut";
    return "ok";
  }
  if (["alive", "active"].includes(normalized)) return "ok";
  if (["sold", "died", "culled", "lost", "inactive"].includes(normalized)) return "dng";
  return "mut";
}

function weightLabel(weight: number | null | undefined): string {
  return typeof weight === "number" && Number.isFinite(weight) ? `${weight.toFixed(weight % 1 === 0 ? 0 : 1)} kg` : "—";
}

function herdGoatId(item: HerdPassportDrawerItem): string {
  return item.goatId;
}

type ReadResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: string };

async function getHerdGoatVaccinationPassport(goatId: string): Promise<ReadResult<VaccinationPassport>> {
  try {
    const response = await fetch(`/api/goats/${encodeURIComponent(goatId)}/vaccination-passport`, {
      headers: { Accept: "application/json" },
      cache: "no-store",
    });
    const payload = await response.json().catch(() => ({})) as Partial<VaccinationPassport> & { error?: string };
    if (!response.ok || !payload.goat_id) {
      return { ok: false, error: payload.error ?? `vaccination_passport_read_${response.status}` };
    }
    return { ok: true, data: payload as VaccinationPassport };
  } catch {
    return { ok: false, error: "vaccination_passport_unreachable" };
  }
}

function obligationTone(status: string): StatusTone {
  if (status === "deferred" || status === "missed") return "warn";
  if (status === "due" || status === "in_progress") return "info";
  if (status === "scheduled") return "mut";
  return "mut";
}

function historyTone(status: string): StatusTone {
  if (status === "accepted") return "ok";
  if (status === "rejected") return "dng";
  if (status === "recorded") return "warn";
  return "mut";
}

function proofLabel(item: VaccinationPassportHistoryItem, pageContract: AdminUiPageContract) {
  if (item.status === "accepted") return <Tag tone="ok">{copy(pageContract, "vaccination.proof_verified")}</Tag>;
  if (item.status === "recorded") return <Tag tone="warn">{copy(pageContract, "vaccination.awaiting_verify")}</Tag>;
  if (item.status === "rejected") return <Tag tone="dng">{copy(pageContract, "vaccination.rework_rejected")}</Tag>;
  return <Tag tone="mut">{item.status}</Tag>;
}

function sourceObligationLabel(obligationId: string): string {
  return obligationId.slice(0, 8);
}

function vaccineRowLabel(item: { display_label: string }): string {
  return item.display_label;
}

function sameDate(left?: string, right?: string): boolean {
  return Boolean(left && right && left.slice(0, 10) === right.slice(0, 10));
}

function passportIdentityLabels(pageContract: AdminUiPageContract): string[] {
  if (pageContract.route_id === "herd-register") return tableLabels(pageContract, "herd-register");
  return [
    copy(pageContract, "calendar.drive.display_id_header"),
    copy(pageContract, "calendar.drive.tag_1_header"),
    copy(pageContract, "calendar.drive.tag_2_header"),
    copy(pageContract, "calendar.drive.park_header"),
    copy(pageContract, "calendar.drive.shed_header"),
    copy(pageContract, "calendar.drive.breed_header"),
    copy(pageContract, "calendar.drive.sex_header"),
    copy(pageContract, "calendar.drive.weight_header"),
    copy(pageContract, "calendar.drive.lifecycle_header"),
    copy(pageContract, "calendar.drive.health_header"),
    copy(pageContract, "calendar.drive.stage_header"),
  ];
}

function HerdDrawerVaccinationBlock({
  vaccination,
  error,
  pageContract,
}: {
  vaccination: VaccinationPassport | undefined;
  error: string | undefined;
  pageContract: AdminUiPageContract;
}) {
  const open = vaccination?.open_obligations ?? [];
  const history = vaccination?.vaccination_history ?? [];
  const openCols = tableLabels(pageContract, "vaccination-open-obligations");
  const historyCols = tableLabels(pageContract, "vaccination-history");

  return (
    <DrawerBlock
      title={
        <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
          <Iconify icon="solar:medical-kit-bold" width={16} aria-hidden="true" />
          {copy(pageContract, "section.vaccination.title")}
        </Box>
      }
    >
      {error ? (
        <Caption>{copy(pageContract, "vaccination.unavailable_prefix")}: {error}</Caption>
      ) : !vaccination ? (
        <Typography variant="body2" sx={{ color: "text.secondary" }} aria-live="polite">...</Typography>
      ) : (
        <>
          <DrawerMetaGrid>
            <DrawerMetaItem label={copy(pageContract, "vaccination.next_due")}>
              {vaccination.next_due ? (
                <>
                  {fmtDate(vaccination.next_due.scheduled_for || vaccination.next_due.due_at)}{" "}
                  <Tag tone={obligationTone(vaccination.next_due.status)}>{vaccination.next_due.status}</Tag>
                </>
              ) : copy(pageContract, "vaccination.no_upcoming")}
            </DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "vaccination.open_obligations")}>{open.length}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "vaccination.last_accepted")} span>
              {vaccination.last_accepted ? fmtDate(vaccination.last_accepted.administered_at) : copy(pageContract, "label.placeholder")}
            </DrawerMetaItem>
          </DrawerMetaGrid>

          <Typography variant="subtitle2">{copy(pageContract, "vaccination.open_due_rows")}</Typography>
          {open.length === 0 ? (
            <Caption>{copy(pageContract, "vaccination.empty_open")}</Caption>
          ) : (
            <Box tabIndex={0} role="group" aria-label={copy(pageContract, "vaccination.open_due_rows")}>
              <DrawerTableScroll>
                <Table size="small" sx={{ minWidth: 440 }}>
                  <TableHead>
                    <TableRow>{openCols.slice(0, 4).map((label) => <TableCell component="th" key={label}>{label}</TableCell>)}</TableRow>
                  </TableHead>
                  <TableBody>
                    {open.slice(0, DRAWER_ROW_LIMIT).map((due) => (
                      <TableRow key={due.obligation_id}>
                        <TableCell>
                          <div>{fmtDate(due.scheduled_for || due.due_at)}</div>
                          {due.scheduled_for && due.clinical_due_at && !sameDate(due.scheduled_for, due.clinical_due_at) ? (
                            <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{copy(pageContract, "vaccination.clinical_due")} {fmtDate(due.clinical_due_at)}</Typography>
                          ) : null}
                        </TableCell>
                        <TableCell>{vaccineRowLabel(due)}</TableCell>
                        <TableCell><Tag tone={obligationTone(due.status)}>{due.status}</Tag></TableCell>
                        <TableCell><Label variant="soft" color="primary" title={due.obligation_id}>{sourceObligationLabel(due.obligation_id)}</Label></TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </DrawerTableScroll>
              {open.length > DRAWER_ROW_LIMIT ? <Caption>+{open.length - DRAWER_ROW_LIMIT} more</Caption> : null}
            </Box>
          )}

          <Typography variant="subtitle2">{copy(pageContract, "vaccination.history")}</Typography>
          {history.length === 0 ? (
            <Caption>{copy(pageContract, "vaccination.empty_history")}</Caption>
          ) : (
            <Box tabIndex={0} role="group" aria-label={copy(pageContract, "table.vaccination.aria")}>
              <DrawerTableScroll>
                <Table size="small" sx={{ minWidth: 520 }}>
                  <TableHead>
                    <TableRow>{historyCols.slice(0, 5).map((label) => <TableCell component="th" key={label}>{label}</TableCell>)}</TableRow>
                  </TableHead>
                  <TableBody>
                    {history.slice(0, DRAWER_ROW_LIMIT).map((h) => (
                      <TableRow key={h.completion_id}>
                        <TableCell>{fmtDate(h.administered_at)}</TableCell>
                        <TableCell>{vaccineRowLabel(h)}</TableCell>
                        <TableCell><Tag tone={historyTone(h.status)}>{h.status}</Tag></TableCell>
                        <TableCell>{proofLabel(h, pageContract)}</TableCell>
                        <TableCell><Label variant="soft" color="primary" title={h.obligation_id}>{sourceObligationLabel(h.obligation_id)}</Label></TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </DrawerTableScroll>
              {history.length > DRAWER_ROW_LIMIT ? <Caption>+{history.length - DRAWER_ROW_LIMIT} more</Caption> : null}
            </Box>
          )}
        </>
      )}
    </DrawerBlock>
  );
}

export function HerdPassportLocalDrawer({
  items,
  initialSelectedId,
  closeHref,
  reproductiveIdempotencyKey,
  returnTo,
  pageContract,
}: {
  items: HerdPassportDrawerItem[];
  initialSelectedId?: string;
  closeHref: string;
  reproductiveIdempotencyKey: string;
  returnTo: string;
  pageContract: AdminUiPageContract;
}) {
  const { displayedItem: item, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items,
    itemId: herdGoatId,
    selectionKey: "goat_passport",
    initialSelectedId,
    closeHref,
  });
  const [vaccinationPassports, setVaccinationPassports] = useState<Record<string, VaccinationPassport>>({});
  const [vaccinationErrors, setVaccinationErrors] = useState<Record<string, string>>({});
  const vaccinationRequestsRef = useRef(new Map<string, ReturnType<typeof getHerdGoatVaccinationPassport>>());

  useEffect(() => {
    if (!item || vaccinationPassports[item.goatId] || vaccinationErrors[item.goatId]) return;
    const goatId = item.goatId;
    let active = true;
    const request = vaccinationRequestsRef.current.get(goatId) ?? getHerdGoatVaccinationPassport(goatId);
    vaccinationRequestsRef.current.set(goatId, request);
    void request.then((result) => {
      if (!active) return;
      if (result.ok) setVaccinationPassports((current) => ({ ...current, [goatId]: result.data }));
      else setVaccinationErrors((current) => ({ ...current, [goatId]: result.error }));
    });
    return () => {
      active = false;
    };
  }, [item, vaccinationErrors, vaccinationPassports]);

  if (!item) return null;
  const vaccination = vaccinationPassports[item.goatId];
  const vaccinationError = vaccinationErrors[item.goatId];
  const cols = passportIdentityLabels(pageContract);
  const canEditReproductiveStatus = pageContract.route_id === "herd-register";

  return (
    <DetailDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={copy(pageContract, "drawer.passport.aria")}
      eyebrow={item.displayId}
      icon={<Typography component="span" variant="subtitle2">G</Typography>}
      ariaLabel={copy(pageContract, "drawer.passport.aria")}
      closeLabel={copy(pageContract, "drawer.passport.close_label")}
      footer={
        <>
          <LinkButton href={`/goats/${encodeURIComponent(item.goatId)}`} variant="contained">{copy(pageContract, "action.full_change_history")}</LinkButton>
          <Button variant="outlined" color="inherit" onClick={closeDrawer}>{copy(pageContract, "action.close")}</Button>
        </>
      }
    >
      <DrawerMetaGrid>
        <DrawerMetaItem label={cols[0]}><Label variant="soft" color="primary">{item.displayId}</Label></DrawerMetaItem>
        <DrawerMetaItem label={cols[1]}>{dash(item.tag1)}</DrawerMetaItem>
        <DrawerMetaItem label={cols[2]}>{dash(item.tag2)}</DrawerMetaItem>
        <DrawerMetaItem label={cols[3]}>{item.park}</DrawerMetaItem>
        <DrawerMetaItem label={cols[4]}>{item.shed}</DrawerMetaItem>
        <DrawerMetaItem label={cols[5]}>{dash(item.breed)}</DrawerMetaItem>
        <DrawerMetaItem label={cols[6]}>{dash(item.sex)}</DrawerMetaItem>
        <DrawerMetaItem label={cols[7]}>{weightLabel(item.weightKg)}</DrawerMetaItem>
        <DrawerMetaItem label={cols[8]}><Tag tone={statusTone(item.lifecycleStatus, "lifecycle")}>{statusLabel(pageContract, "herd_lifecycle", item.lifecycleStatus)}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={cols[9]}><Tag tone={statusTone(item.healthStatus, "health")}>{statusLabel(pageContract, "herd_health", item.healthStatus)}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={cols[10]} span>
          <Box component="span" sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
            <Tag tone={statusTone(item.reproductiveStatus, "breeding")}>{statusLabel(pageContract, "herd_reproductive", item.reproductiveStatus)}</Tag>
            {canEditReproductiveStatus ? (
              <HerdReproductiveEdit
                goatId={item.goatId}
                displayId={item.displayId}
                currentStatus={item.reproductiveStatus}
                idempotencyKey={reproductiveIdempotencyKey}
                returnTo={returnTo}
                pageContract={pageContract}
              />
            ) : null}
          </Box>
        </DrawerMetaItem>
      </DrawerMetaGrid>
      <HerdDrawerVaccinationBlock vaccination={vaccination} error={vaccinationError} pageContract={pageContract} />
    </DetailDrawer>
  );
}
