"use client";

import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { GoatPassportResponse, VaccinationPassport, VaccinationPassportHistoryItem } from "@/lib/api/server";
import type { VaccinationShedAnimalRow } from "@/lib/api/vaccination-sheds";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { dash, fmtDate } from "@/lib/format";
import { Syringe, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

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
    <div style={{ marginTop: 16 }}>
      <div
        className="muted small"
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          fontWeight: 700,
          textTransform: "uppercase",
          letterSpacing: ".4px",
          marginBottom: 10,
        }}
      >
        <Syringe className="ic" aria-hidden="true" style={{ width: 14, height: 14 }} />
        {copy(pageContract, "section.vaccination.title")}
      </div>

      {error ? (
        <p className="muted small" style={{ marginTop: 8 }}>
          {copy(pageContract, "vaccination.unavailable_prefix")}: {error}
        </p>
      ) : !vaccination ? (
        <p className="muted small" style={{ margin: 0 }} aria-live="polite">...</p>
      ) : (
        <>
          <div className="metagrid" style={{ gridTemplateColumns: "1fr 1fr 1fr", marginBottom: 12 }}>
            <div>
              <div className="k">{copy(pageContract, "vaccination.next_due")}</div>
              <div className="v" style={{ fontSize: 13 }}>
                {vaccination.next_due ? (
                  <>
                    {fmtDate(vaccination.next_due.due_at)}{" "}
                    <Tag tone={obligationTone(vaccination.next_due.status)}>{vaccination.next_due.status}</Tag>
                  </>
                ) : (
                  copy(pageContract, "vaccination.no_upcoming")
                )}
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "vaccination.open_obligations")}</div>
              <div className="v">{open.length}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "vaccination.last_accepted")}</div>
              <div className="v" style={{ fontSize: 13 }}>
                {vaccination.last_accepted ? fmtDate(vaccination.last_accepted.administered_at) : copy(pageContract, "label.placeholder")}
              </div>
            </div>
          </div>

          <div className="muted small" style={{ fontWeight: 700, marginBottom: 6 }}>
            {copy(pageContract, "vaccination.open_due_rows")}
          </div>
          {open.length === 0 ? (
            <p className="muted small" style={{ margin: "0 0 12px" }}>
              {copy(pageContract, "vaccination.empty_open")}
            </p>
          ) : (
            <div style={{ overflowX: "auto", marginBottom: 12 }} tabIndex={0} role="group" aria-label={copy(pageContract, "vaccination.open_due_rows")}>
              <table>
                <thead>
                  <tr>
                    {openCols.slice(0, 4).map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {open.slice(0, DRAWER_ROW_LIMIT).map((due) => {
                    const rowId = realWorkflowRowId(due.workflow_row_id);
                    return (
                      <tr key={due.obligation_id}>
                        <td>{fmtDate(due.due_at)}</td>
                        <td>{due.sequence}</td>
                        <td><Tag tone={obligationTone(due.status)}>{due.status}</Tag></td>
                        <td>
                          {rowId ? (
                            <Link href={workflowHref(rowId)} className="lk small">
                              {copy(pageContract, "action.open_workflow")} →
                            </Link>
                          ) : (
                            <span className="gid" title={due.obligation_id}>{sourceObligationLabel(due.obligation_id)}</span>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              {open.length > DRAWER_ROW_LIMIT ? (
                <p className="muted small" style={{ margin: "6px 0 0" }}>
                  +{open.length - DRAWER_ROW_LIMIT} more
                </p>
              ) : null}
            </div>
          )}

          <div className="muted small" style={{ fontWeight: 700, marginBottom: 6 }}>
            {copy(pageContract, "vaccination.history")}
          </div>
          {history.length === 0 ? (
            <p className="muted small" style={{ margin: 0 }}>
              {copy(pageContract, "vaccination.empty_history")}
            </p>
          ) : (
            <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.vaccination.aria")}>
              <table>
                <thead>
                  <tr>
                    {historyCols.slice(0, 5).map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {history.slice(0, DRAWER_ROW_LIMIT).map((item) => (
                    <tr key={item.completion_id}>
                      <td>{fmtDate(item.administered_at)}</td>
                      <td>{item.doses}</td>
                      <td><Tag tone={historyTone(item.status)}>{item.status}</Tag></td>
                      <td>{proofLabel(item, pageContract)}</td>
                      <td><span className="gid" title={item.obligation_id}>{sourceObligationLabel(item.obligation_id)}</span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {history.length > DRAWER_ROW_LIMIT ? (
                <p className="muted small" style={{ margin: "6px 0 0" }}>
                  +{history.length - DRAWER_ROW_LIMIT} more
                </p>
              ) : null}
            </div>
          )}
        </>
      )}
    </div>
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
  const { displayedItem, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
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

  return (
    <>
      <button
        type="button"
        className={`scrim${drawerOpen ? " on" : ""}`}
        aria-label={copy(pageContract, "drawer.passport.close_label")}
        aria-hidden={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <aside className={`drawer${drawerOpen ? " on" : ""}`} aria-label={copy(pageContract, "drawer.passport.aria")} aria-hidden={!drawerOpen} inert={!drawerOpen}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)", fontWeight: 800 }}>G</span>
          <div>
            <div className="mt">{goat?.display_id ?? displayedItem.displayId}</div>
            <h2>{copy(pageContract, "drawer.passport.aria")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.passport.close_label")} onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>
        <div className="dc">
          {error ? (
            <div className="alert"><b>{copy(pageContract, "fallback.title")}</b>&nbsp;{error}</div>
          ) : (
            <>
              <div className="helpgrid" style={{ marginBottom: 12 }}>
                <div className="hk">{copy(pageContract, "label.display_id")}</div>
                <div><span className="gid">{goat?.display_id ?? displayedItem.displayId}</span></div>
                <div className="hk">{copy(pageContract, "label.tag_1")}</div>
                <div className="mono">{dash(goat?.summary.animal_identifier_1 ?? displayedItem.tag1)}</div>
                <div className="hk">{copy(pageContract, "label.tag_2")}</div>
                <div className="mono">{dash(goat?.summary.animal_identifier_2 ?? displayedItem.tag2)}</div>
              </div>
              <div className="helpgrid">
                <div className="hk">{copy(pageContract, "label.location")}</div>
                <div>{dash(goat?.summary.location_path.display)}</div>
                <div className="hk">{copy(pageContract, "label.breed_sex")}</div>
                <div>{dash(goat ? [goat.summary.breed, goat.summary.sex].filter(Boolean).join(" / ") : [displayedItem.breed, displayedItem.sex].filter(Boolean).join(" / "))}</div>
                <div className="hk">{copy(pageContract, "label.lifecycle")}</div>
                <div><Tag tone={statusTone(goat?.summary.lifecycle_status ?? displayedItem.lifecycleStatus, "lifecycle")}>{dash(goat?.summary.lifecycle_status ?? displayedItem.lifecycleStatus)}</Tag></div>
                <div className="hk">{copy(pageContract, "label.health")}</div>
                <div><Tag tone={statusTone(goat?.summary.health_status ?? displayedItem.healthStatus, "health")}>{dash(goat?.summary.health_status ?? displayedItem.healthStatus)}</Tag></div>
                <div className="hk">{copy(pageContract, "label.reproductive")}</div>
                <div><Tag tone={statusTone(goat?.summary.reproductive_status, "breeding")}>{dash(goat?.summary.reproductive_status)}</Tag></div>
              </div>
              <DrawerVaccinationBlock
                vaccination={vaccination}
                error={vaccinationError}
                pageContract={pageContract}
              />
              {!goat ? <p className="muted small" style={{ marginTop: 14 }} aria-live="polite">…</p> : null}
            </>
          )}
        </div>
        <div className="df">
          <Link href={`/goats/${encodeURIComponent(displayedItem.goatId)}`} className="btn p">
            {copy(pageContract, "action.full_change_history")}
          </Link>
          <button type="button" className="btn" onClick={closeDrawer}>{copy(pageContract, "action.close")}</button>
        </div>
      </aside>
    </>
  );
}
