"use client";

import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Syringe, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Tag } from "@/components/ui-primitives";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { dash, fmtDate } from "@/lib/format";
import type { VaccinationPassport, VaccinationPassportHistoryItem } from "@/lib/api/server";
import { HerdReproductiveEdit } from "./herd-actions-ui";

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
  const response = await fetch(`/api/goats/${encodeURIComponent(goatId)}/vaccination-passport`, {
    headers: { Accept: "application/json" },
    cache: "no-store",
  });
  const payload = await response.json() as Partial<VaccinationPassport> & { error?: string };
  if (!response.ok || !payload.goat_id) {
    return { ok: false, error: payload.error ?? `vaccination_passport_read_${response.status}` };
  }
  return { ok: true, data: payload as VaccinationPassport };
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
    <div style={{ marginTop: 16 }}>
      <div className="muted small" style={{ display: "flex", alignItems: "center", gap: 6, fontWeight: 700, textTransform: "uppercase", letterSpacing: ".4px", marginBottom: 10 }}>
        <Syringe className="ic" aria-hidden="true" style={{ width: 14, height: 14 }} />
        {copy(pageContract, "section.vaccination.title")}
      </div>
      {error ? (
        <p className="muted small" style={{ marginTop: 8 }}>{copy(pageContract, "vaccination.unavailable_prefix")}: {error}</p>
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
                ) : copy(pageContract, "vaccination.no_upcoming")}
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

          <div className="muted small" style={{ fontWeight: 700, marginBottom: 6 }}>{copy(pageContract, "vaccination.open_due_rows")}</div>
          {open.length === 0 ? (
            <p className="muted small" style={{ margin: "0 0 12px" }}>{copy(pageContract, "vaccination.empty_open")}</p>
          ) : (
            <div style={{ overflowX: "auto", marginBottom: 12 }} tabIndex={0} role="group" aria-label={copy(pageContract, "vaccination.open_due_rows")}>
              <table>
                <thead>
                  <tr>{openCols.slice(0, 4).map((label) => <th key={label}>{label}</th>)}</tr>
                </thead>
                <tbody>
                  {open.slice(0, DRAWER_ROW_LIMIT).map((due) => (
                    <tr key={due.obligation_id}>
                      <td>{fmtDate(due.due_at)}</td>
                      <td>{due.sequence}</td>
                      <td><Tag tone={obligationTone(due.status)}>{due.status}</Tag></td>
                      <td><span className="gid" title={due.obligation_id}>{sourceObligationLabel(due.obligation_id)}</span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {open.length > DRAWER_ROW_LIMIT ? <p className="muted small" style={{ margin: "6px 0 0" }}>+{open.length - DRAWER_ROW_LIMIT} more</p> : null}
            </div>
          )}

          <div className="muted small" style={{ fontWeight: 700, marginBottom: 6 }}>{copy(pageContract, "vaccination.history")}</div>
          {history.length === 0 ? (
            <p className="muted small" style={{ margin: 0 }}>{copy(pageContract, "vaccination.empty_history")}</p>
          ) : (
            <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.vaccination.aria")}>
              <table>
                <thead>
                  <tr>{historyCols.slice(0, 5).map((label) => <th key={label}>{label}</th>)}</tr>
                </thead>
                <tbody>
                  {history.slice(0, DRAWER_ROW_LIMIT).map((h) => (
                    <tr key={h.completion_id}>
                      <td>{fmtDate(h.administered_at)}</td>
                      <td>{h.doses}</td>
                      <td><Tag tone={historyTone(h.status)}>{h.status}</Tag></td>
                      <td>{proofLabel(h, pageContract)}</td>
                      <td><span className="gid" title={h.obligation_id}>{sourceObligationLabel(h.obligation_id)}</span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {history.length > DRAWER_ROW_LIMIT ? <p className="muted small" style={{ margin: "6px 0 0" }}>+{history.length - DRAWER_ROW_LIMIT} more</p> : null}
            </div>
          )}
        </>
      )}
    </div>
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
  const { displayedItem: item, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
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
  const cols = tableLabels(pageContract, "herd-register");

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
            <div className="mt">{item.displayId}</div>
            <h2>{copy(pageContract, "drawer.passport.aria")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.passport.close_label")} onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>
        <div className="dc">
          <div className="helpgrid" style={{ marginBottom: 12 }}>
            <div className="hk">{cols[0]}</div>
            <div><span className="gid">{item.displayId}</span></div>
            <div className="hk">{cols[1]}</div>
            <div className="mono">{dash(item.tag1)}</div>
            <div className="hk">{cols[2]}</div>
            <div className="mono">{dash(item.tag2)}</div>
          </div>
          <div className="helpgrid">
            <div className="hk">{cols[3]}</div><div>{item.park}</div>
            <div className="hk">{cols[4]}</div><div>{item.shed}</div>
            <div className="hk">{cols[5]}</div><div>{dash(item.breed)}</div>
            <div className="hk">{cols[6]}</div><div>{dash(item.sex)}</div>
            <div className="hk">{cols[7]}</div><div>{weightLabel(item.weightKg)}</div>
            <div className="hk">{cols[8]}</div><div><Tag tone={statusTone(item.lifecycleStatus, "lifecycle")}>{dash(item.lifecycleStatus)}</Tag></div>
            <div className="hk">{cols[9]}</div><div><Tag tone={statusTone(item.healthStatus, "health")}>{dash(item.healthStatus)}</Tag></div>
            <div className="hk">{cols[10]}</div>
            <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <Tag tone={statusTone(item.reproductiveStatus, "breeding")}>{dash(item.reproductiveStatus)}</Tag>
              <HerdReproductiveEdit
                goatId={item.goatId}
                displayId={item.displayId}
                currentStatus={item.reproductiveStatus}
                idempotencyKey={reproductiveIdempotencyKey}
                returnTo={returnTo}
                pageContract={pageContract}
              />
            </div>
          </div>
          <HerdDrawerVaccinationBlock vaccination={vaccination} error={vaccinationError} pageContract={pageContract} />
        </div>
        <div className="df">
          <Link href={`/goats/${encodeURIComponent(item.goatId)}`} className="btn p">{copy(pageContract, "action.full_change_history")}</Link>
          <button type="button" className="btn" onClick={closeDrawer}>{copy(pageContract, "action.close")}</button>
        </div>
      </aside>
    </>
  );
}
