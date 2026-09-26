"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { GoatPassportResponse, VaccinationPassport, VaccinationPassportHistoryItem } from "@/lib/api/server";
import type { VaccinationShedAnimalRow } from "@/lib/api/vaccination-sheds";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { dash, fmtDate } from "@/lib/format";
import { Syringe } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import { LinkButton } from "@/components/minimal/link-button";
import { DetailDrawer, DrawerBlock, DrawerMetaGrid, DrawerMetaItem, DrawerTableScroll } from "@/components/app/detail-drawer";

type GoatPassport = GoatPassportResponse["goat"];
const DRAWER_ROW_LIMIT = 5;

function animalId(row: VaccinationShedAnimalRow): string {
  return row.goatId;
}

type ReadResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: string };

async function getShedGoatPassport(goatId: string): Promise<ReadResult<GoatPassportResponse>> {
  try {
    const response = await fetch(`/api/goats/${encodeURIComponent(goatId)}/passport`, {
      headers: { Accept: "application/json" },
      cache: "no-store",
    });
    const payload = await response.json().catch(() => ({})) as Partial<GoatPassportResponse> & { error?: string };
    if (!response.ok || !payload.goat) {
      return { ok: false, error: payload.error ?? `passport_read_${response.status}` };
    }
    return { ok: true, data: payload as GoatPassportResponse };
  } catch {
    return { ok: false, error: "passport_unreachable" };
  }
}

async function getShedGoatVaccinationPassport(goatId: string): Promise<ReadResult<VaccinationPassport>> {
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

function statusTone(value: string | null | undefined, kind: "lifecycle" | "health" | "breeding"): "ok" | "warn" | "dng" | "info" | "mut" {
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

function obligationTone(status: string): "ok" | "warn" | "dng" | "info" | "mut" {
  if (status === "deferred" || status === "missed") return "warn";
  if (status === "due" || status === "in_progress") return "info";
  if (status === "scheduled") return "mut";
  return "mut";
}

function historyTone(status: string): "ok" | "warn" | "dng" | "info" | "mut" {
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

function realWorkflowRowId(rowId: string | undefined): string | null {
  const trimmed = rowId?.trim();
  if (!trimmed || trimmed.startsWith("obligation:")) return null;
  return trimmed;
}

function workflowHref(rowId: string): string {
  return `/workflows/${encodeURIComponent(rowId)}`;
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

function DrawerVaccinationBlock({
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
          <Syringe size={16} aria-hidden="true" />
          {copy(pageContract, "section.vaccination.title")}
        </Box>
      }
    >
      {error ? (
        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          {copy(pageContract, "vaccination.unavailable_prefix")}: {error}
        </Typography>
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
              ) : (
                copy(pageContract, "vaccination.no_upcoming")
              )}
            </DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "vaccination.open_obligations")}>{open.length}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "vaccination.last_accepted")} span>
              {vaccination.last_accepted ? fmtDate(vaccination.last_accepted.administered_at) : copy(pageContract, "label.placeholder")}
            </DrawerMetaItem>
          </DrawerMetaGrid>

          <Typography variant="subtitle2">{copy(pageContract, "vaccination.open_due_rows")}</Typography>
          {open.length === 0 ? (
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "vaccination.empty_open")}
            </Typography>
          ) : (
            <Box tabIndex={0} role="group" aria-label={copy(pageContract, "vaccination.open_due_rows")}>
              <DrawerTableScroll>
                <Table size="small" sx={{ minWidth: 440 }}>
                  <TableHead>
                    <TableRow>
                      {openCols.slice(0, 4).map((label) => (
                        <TableCell component="th" key={label}>{label}</TableCell>
                      ))}
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {open.slice(0, DRAWER_ROW_LIMIT).map((due) => {
                      const rowId = realWorkflowRowId(due.workflow_row_id);
                      return (
                        <TableRow key={due.obligation_id}>
                          <TableCell>
                            <div>{fmtDate(due.scheduled_for || due.due_at)}</div>
                            {due.scheduled_for && due.clinical_due_at && !sameDate(due.scheduled_for, due.clinical_due_at) ? (
                              <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{copy(pageContract, "vaccination.clinical_due")} {fmtDate(due.clinical_due_at)}</Typography>
                            ) : null}
                          </TableCell>
                          <TableCell>{vaccineRowLabel(due)}</TableCell>
                          <TableCell><Tag tone={obligationTone(due.status)}>{due.status}</Tag></TableCell>
                          <TableCell>
                            {rowId ? (
                              <Link href={workflowHref(rowId)} className="lk small">
                                {copy(pageContract, "action.open_workflow")} →
                              </Link>
                            ) : (
                              <span className="gid" title={due.obligation_id}>{sourceObligationLabel(due.obligation_id)}</span>
                            )}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </DrawerTableScroll>
              {open.length > DRAWER_ROW_LIMIT ? (
                <Typography variant="caption" component="p" sx={{ color: "text.secondary", mt: 1 }}>
                  +{open.length - DRAWER_ROW_LIMIT} more
                </Typography>
              ) : null}
            </Box>
          )}

          <Typography variant="subtitle2">{copy(pageContract, "vaccination.history")}</Typography>
          {history.length === 0 ? (
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "vaccination.empty_history")}
            </Typography>
          ) : (
            <Box tabIndex={0} role="group" aria-label={copy(pageContract, "table.vaccination.aria")}>
              <DrawerTableScroll>
                <Table size="small" sx={{ minWidth: 520 }}>
                  <TableHead>
                    <TableRow>
                      {historyCols.slice(0, 5).map((label) => (
                        <TableCell component="th" key={label}>{label}</TableCell>
                      ))}
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {history.slice(0, DRAWER_ROW_LIMIT).map((item) => (
                      <TableRow key={item.completion_id}>
                        <TableCell>{fmtDate(item.administered_at)}</TableCell>
                        <TableCell>{vaccineRowLabel(item)}</TableCell>
                        <TableCell><Tag tone={historyTone(item.status)}>{item.status}</Tag></TableCell>
                        <TableCell>{proofLabel(item, pageContract)}</TableCell>
                        <TableCell><span className="gid" title={item.obligation_id}>{sourceObligationLabel(item.obligation_id)}</span></TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </DrawerTableScroll>
              {history.length > DRAWER_ROW_LIMIT ? (
                <Typography variant="caption" component="p" sx={{ color: "text.secondary", mt: 1 }}>
                  +{history.length - DRAWER_ROW_LIMIT} more
                </Typography>
              ) : null}
            </Box>
          )}
        </>
      )}
    </DrawerBlock>
  );
}

export function ShedPassportLocalDrawer({
  rows,
  initialSelectedGoatId,
  closeHref,
  pageContract,
}: {
  rows: VaccinationShedAnimalRow[];
  initialSelectedGoatId?: string;
  closeHref: string;
  pageContract: AdminUiPageContract;
}) {
  const { displayedItem, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: rows,
    itemId: animalId,
    selectionKey: "goat_passport",
    initialSelectedId: initialSelectedGoatId,
    closeHref,
  });
  const [passports, setPassports] = useState<Record<string, GoatPassport>>({});
  const [vaccinationPassports, setVaccinationPassports] = useState<Record<string, VaccinationPassport>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [vaccinationErrors, setVaccinationErrors] = useState<Record<string, string>>({});
  const requestsRef = useRef(new Map<string, ReturnType<typeof getShedGoatPassport>>());
  const vaccinationRequestsRef = useRef(new Map<string, ReturnType<typeof getShedGoatVaccinationPassport>>());

  useEffect(() => {
    if (!displayedItem || passports[displayedItem.goatId] || errors[displayedItem.goatId]) return;
    const goatId = displayedItem.goatId;
    let active = true;
    const request = requestsRef.current.get(goatId) ?? getShedGoatPassport(goatId);
    requestsRef.current.set(goatId, request);
    void request.then((result) => {
      if (!active) return;
      if (result.ok) setPassports((current) => ({ ...current, [goatId]: result.data.goat }));
      else setErrors((current) => ({ ...current, [goatId]: result.error }));
    });
    return () => {
      active = false;
    };
  }, [displayedItem, errors, passports]);

  useEffect(() => {
    if (!displayedItem || vaccinationPassports[displayedItem.goatId] || vaccinationErrors[displayedItem.goatId]) return;
    const goatId = displayedItem.goatId;
    let active = true;
    const request = vaccinationRequestsRef.current.get(goatId) ?? getShedGoatVaccinationPassport(goatId);
    vaccinationRequestsRef.current.set(goatId, request);
    void request.then((result) => {
      if (!active) return;
      if (result.ok) setVaccinationPassports((current) => ({ ...current, [goatId]: result.data }));
      else setVaccinationErrors((current) => ({ ...current, [goatId]: result.error }));
    });
    return () => {
      active = false;
    };
  }, [displayedItem, vaccinationErrors, vaccinationPassports]);

  if (!displayedItem) return null;
  const goat = passports[displayedItem.goatId];
  const error = errors[displayedItem.goatId];
  const vaccination = vaccinationPassports[displayedItem.goatId];
  const vaccinationError = vaccinationErrors[displayedItem.goatId];

  const summary = goat?.summary;
  return (
    <DetailDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={copy(pageContract, "drawer.passport.aria")}
      eyebrow={goat?.display_id ?? displayedItem.displayId}
      icon={<Typography component="span" variant="subtitle2">G</Typography>}
      ariaLabel={copy(pageContract, "drawer.passport.aria")}
      closeLabel={copy(pageContract, "drawer.passport.close_label")}
      footer={
        <>
          <LinkButton href={`/goats/${encodeURIComponent(displayedItem.goatId)}`} variant="contained">
            {copy(pageContract, "action.full_change_history")}
          </LinkButton>
          <Button variant="outlined" color="inherit" onClick={closeDrawer}>{copy(pageContract, "action.close")}</Button>
        </>
      }
    >
      {error ? (
        <Alert severity="error"><b>{copy(pageContract, "fallback.title")}</b>&nbsp;{error}</Alert>
      ) : (
        <>
          <DrawerMetaGrid>
            <DrawerMetaItem label={copy(pageContract, "label.display_id")}><span className="gid">{goat?.display_id ?? displayedItem.displayId}</span></DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "label.tag_1")}>{dash(summary?.animal_identifier_1 ?? displayedItem.tag1)}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "label.tag_2")}>{dash(summary?.animal_identifier_2 ?? displayedItem.tag2)}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "label.location")}>{dash(summary?.location_path.operational_location_display)}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "label.breed_sex")}>{dash(goat ? [goat.summary.breed, goat.summary.sex].filter(Boolean).join(" / ") : [displayedItem.breed, displayedItem.sex].filter(Boolean).join(" / "))}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "label.lifecycle")}><Tag tone={statusTone(summary?.lifecycle_status ?? displayedItem.lifecycleStatus, "lifecycle")}>{dash(summary?.lifecycle_status ?? displayedItem.lifecycleStatus)}</Tag></DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "label.health")}><Tag tone={statusTone(summary?.health_status ?? displayedItem.healthStatus, "health")}>{dash(summary?.health_status ?? displayedItem.healthStatus)}</Tag></DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "label.reproductive")}><Tag tone={statusTone(summary?.reproductive_status, "breeding")}>{dash(summary?.reproductive_status)}</Tag></DrawerMetaItem>
          </DrawerMetaGrid>
          <DrawerVaccinationBlock
            vaccination={vaccination}
            error={vaccinationError}
            pageContract={pageContract}
          />
          {!goat ? <Typography variant="body2" sx={{ color: "text.secondary" }} aria-live="polite">…</Typography> : null}
        </>
      )}
    </DetailDrawer>
  );
}
