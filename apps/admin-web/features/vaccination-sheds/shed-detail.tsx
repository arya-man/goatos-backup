import Link from "next/link";
import { redirect } from "next/navigation";
import { ArrowLeft, CalendarClock, Syringe, UserRound, Warehouse } from "lucide-react";
import {
  getVaccinationShedAnimals,
  getVaccinationShedDetail,
} from "@/lib/api/server";
import type {
  VaccinationCapacityStatus,
  VaccinationOperationsCounts,
  VaccinationPlannedSession,
  VaccinationShedAnimalRow,
  VaccinationShedDetail,
  VaccinationShedVaccineRow,
} from "@/lib/api/vaccination-sheds";
import { Tag, InfoTooltip, ClipText, type Tone } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { copy, optionLabel, optionTone, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseScope, scopeHref } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";

const ANIMAL_PAGE_SIZE = 100;

// Nonzero obligation counts to surface in the vaccine-breakdown Counts cell. Labels come from the
// work_state_filter_chips contract group (never hardcoded); `accepted` maps to the "completed" chip.
const COUNT_CHIPS: { field: keyof VaccinationOperationsCounts; key: string }[] = [
  { field: "overdue", key: "overdue" },
  { field: "due", key: "due" },
  { field: "proofPending", key: "proof_pending" },
  { field: "rejected", key: "rejected" },
  { field: "accepted", key: "completed" },
];

function NotFoundOrError({ shedId, message, backHref, pageContract }: { shedId: string; message: string; backHref: string; pageContract: AdminUiPageContract }) {
  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <Link href={backHref} className="lk">
              {copy(pageContract, "crumb")}
            </Link>
          </div>
          <h1>{copy(pageContract, "fallback.title")}</h1>
          <div className="sub">{message}</div>
        </div>
      </div>
      <section className="card">
        <div className="bd">
          <p className="muted small" style={{ marginBottom: 12 }}>
            {shedId}: {copy(pageContract, "fallback.body")}
          </p>
          <Link href={backHref} className="btn">
            <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {copy(pageContract, "action.back")}
          </Link>
        </div>
      </section>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <div className="k">{label}</div>
      <div className="v">{value}</div>
    </div>
  );
}

function PlannedSessionsCard({ detail, pageContract }: { detail: VaccinationShedDetail; pageContract: AdminUiPageContract }) {
  const cols = table(pageContract, "planned-sessions").columns.filter((c) => c.visible);
  const sessions: VaccinationPlannedSession[] = detail.plannedSessions ?? [];
  return (
    <section className="card" style={{ marginBottom: 14 }}>
      <div className="hd">
        <CalendarClock className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.planned_sessions.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.planned_sessions.note")}</span>
      </div>
      {sessions.length === 0 ? (
        <div className="bd">
          <span className="muted small">{copy(pageContract, "section.planned_sessions.empty")}</span>
        </div>
      ) : (
        <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
          <table>
            <thead>
              <tr>
                {cols.map((col) => (
                  <th key={col.key}>
                    {col.key === "capacity" ? (
                      <span style={{ display: "inline-flex", alignItems: "center" }}>
                        {col.label}
                        <InfoTooltip label={copy(pageContract, "tooltip.capacity.label")}>
                          {copy(pageContract, "tooltip.capacity.body")}
                        </InfoTooltip>
                      </span>
                    ) : (
                      col.label
                    )}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {sessions.map((s, idx) => (
                <tr key={`${s.date}-${idx}`}>
                  <td>{fmtDate(s.date)}</td>
                  <td>{s.vaccinations}</td>
                  <td className="muted">{s.dailyLimit}</td>
                  <td>
                    <Tag tone={optionTone(pageContract, "capacity_chips", s.capacity) as Tone}>
                      {optionLabel(pageContract, "capacity_chips", s.capacity)}
                    </Tag>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function VaccineBreakdownCard({ detail, pageContract }: { detail: VaccinationShedDetail; pageContract: AdminUiPageContract }) {
  const cols = tableLabels(pageContract, "shed-vaccines");
  const vaccines: VaccinationShedVaccineRow[] = detail.vaccines ?? [];
  return (
    <section className="card" style={{ marginBottom: 14 }}>
      <div className="hd">
        <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.vaccines.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.vaccines.note")}</span>
      </div>
      {vaccines.length === 0 ? (
        <div className="bd">
          <span className="muted small">{copy(pageContract, "section.vaccines.empty")}</span>
        </div>
      ) : (
        <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
          <table>
            <thead>
              <tr>
                {cols.map((c) => (
                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {vaccines.map((v) => (
                <tr key={v.protocolId}>
                  <td>
                    <ClipText title={v.name}>{v.name}</ClipText>
                  </td>
                  <td>
                    <Tag tone={optionTone(pageContract, "work_state_filter_chips", v.workState) as Tone}>
                      {optionLabel(pageContract, "work_state_filter_chips", v.workState)}
                    </Tag>
                  </td>
                  <td className="muted">{v.lastDose ? fmtDate(v.lastDose) : copy(pageContract, "label.placeholder")}</td>
                  <td className="muted">{v.nextDue ? fmtDate(v.nextDue) : copy(pageContract, "label.placeholder")}</td>
                  <td>
                    <div style={{ display: "flex", flexWrap: "wrap", gap: 6, alignItems: "center" }}>
                      <span className="muted small">{v.counts.total}</span>
                      {COUNT_CHIPS.filter(({ field }) => (v.counts[field] ?? 0) > 0).map(({ field, key }) => (
                        <Tag key={key} tone={optionTone(pageContract, "work_state_filter_chips", key) as Tone}>
                          {optionLabel(pageContract, "work_state_filter_chips", key)}: {v.counts[field]}
                        </Tag>
                      ))}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function AnimalRosterCard({
  rows,
  nextCursor,
  loadMoreHref,
  pageContract,
}: {
  rows: VaccinationShedAnimalRow[];
  nextCursor: string | null;
  loadMoreHref: string | null;
  pageContract: AdminUiPageContract;
}) {
  const cols = tableLabels(pageContract, "shed-animals");
  return (
    <section id="animals" className="card" style={{ scrollMarginTop: 80 }}>
      <div className="hd">
        <UserRound className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.animals.title")}</h3>
        <Tag tone="mut">{rows.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.animals.note")}</span>
      </div>
      {rows.length === 0 ? (
        <div className="bd">
          <span className="muted small">{copy(pageContract, "section.animals.empty")}</span>
        </div>
      ) : (
        <>
          <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
            <table>
              <thead>
                <tr>
                  {cols.map((c) => (
                    <th key={c}>{c}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((a) => (
                  <tr key={a.goatId}>
                    <td>
                      <span className="gid">{a.displayId}</span>
                    </td>
                    <td className="muted">{a.tag1 ?? copy(pageContract, "label.placeholder")}</td>
                    <td className="muted">{a.tag2 ?? copy(pageContract, "label.placeholder")}</td>
                    <td className="muted">{a.breed ?? copy(pageContract, "label.placeholder")}</td>
                    <td className="muted">{a.sex}</td>
                    <td className="muted">{a.age ?? copy(pageContract, "label.placeholder")}</td>
                    <td>
                      <ClipText title={a.status}>{a.status}</ClipText>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {nextCursor && loadMoreHref ? (
            <div className="bd" style={{ paddingTop: 12 }}>
              <Link href={loadMoreHref} scroll={false} className="btn sm">
                {copy(pageContract, "action.load_more")}
              </Link>
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}

// Shed-wise vaccination detail — planned sessions (with capacity), per-vaccine breakdown, and the shed's
// animal roster. Route: /vaccination/execution/sheds/{shedId}. Back returns to the board's exact
// filtered/paginated state via the ?ret param the board attached; falls back to /vaccination#sheds.
export async function VaccinationShedDetailPage({
  shedId,
  searchParams,
  pageContract,
}: {
  shedId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const ret = one(sp, "ret");
  const animalsCursor = one(sp, "animals_cursor");

  const [detailResult, animalsResult] = await Promise.all([
    getVaccinationShedDetail(shedId),
    getVaccinationShedAnimals(shedId, { cursor: animalsCursor, limit: ANIMAL_PAGE_SIZE }),
  ]);

  const fallbackBack = ret && ret.startsWith("/vaccination") ? ret : `${scopeHref("/vaccination", scope)}#sheds`;
  if (!detailResult.ok) {
    return <NotFoundOrError shedId={shedId} message={detailResult.error.message} backHref={fallbackBack} pageContract={pageContract} />;
  }
  const detail = detailResult.data;
  // Keep the top bar showing this shed's park (mirrors the old execution detail): normalize to park scope.
  if (scope.mode !== "park" && detail.parkId) {
    const preserve: Record<string, string | undefined> = { ret };
    redirect(scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(shedId)}`, scope, { mode: "park", park: detail.parkId }, preserve));
  }
  const backHref = ret && ret.startsWith("/vaccination") ? ret : `${scopeHref("/vaccination", scope, { mode: "park", park: detail.parkId })}#sheds`;

  const animals: VaccinationShedAnimalRow[] = animalsResult.ok ? animalsResult.data.rows : [];
  const nextCursor = animalsResult.ok ? animalsResult.data.nextCursor ?? null : null;
  const loadMoreHref = nextCursor
    ? scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(shedId)}`, scope, { mode: "park", park: detail.parkId }, { ret, animals_cursor: nextCursor }) + "#animals"
    : null;

  const statusTone = optionTone(pageContract, "shed_status_chips", detail.status) as Tone;
  const capacityTone = optionTone(pageContract, "capacity_chips", detail.capacity as VaccinationCapacityStatus) as Tone;

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <Link href={backHref} className="lk">
              {copy(pageContract, "crumb")}
            </Link>{" "}
            · {detail.parkName} · <b>{detail.shedName}</b>
          </div>
          <h1 style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Warehouse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            {detail.parkName} · {detail.shedName}
          </h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href={backHref} className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {copy(pageContract, "action.back")}
        </Link>
      </div>

      {/* Shed overview — animal-level counts + planned sessions + merged status + capacity headline. */}
      <section className="card" style={{ marginBottom: 14 }}>
        <div className="hd">
          <Warehouse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.overview.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <Tag tone={statusTone}>{optionLabel(pageContract, "shed_status_chips", detail.status)}</Tag>
          <Tag tone={capacityTone}>{optionLabel(pageContract, "capacity_chips", detail.capacity as VaccinationCapacityStatus)}</Tag>
        </div>
        <div className="bd">
          <div className="metagrid" style={{ gridTemplateColumns: "repeat(auto-fill,minmax(120px,1fr))", gap: 14 }}>
            <Stat label={copy(pageContract, "label.animals")} value={detail.animals} />
            <Stat label={copy(pageContract, "label.due")} value={detail.due} />
            <Stat label={copy(pageContract, "label.done_stat")} value={detail.done} />
            <Stat label={copy(pageContract, "label.sessions")} value={detail.sessions} />
          </div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 18, marginTop: 14 }}>
            <div>
              <div className="k">{copy(pageContract, "label.manager")}</div>
              <div className="v">
                {detail.manager?.displayName ?? <Tag tone="dng">{copy(pageContract, "label.manager_unassigned")}</Tag>}
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.backup")}</div>
              <div className="v">
                {detail.backup?.displayName ?? <Tag tone="dng">{copy(pageContract, "label.backup_unassigned")}</Tag>}
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Planned sessions ABOVE the vaccine breakdown (capacity/session-splitting plan). */}
      <PlannedSessionsCard detail={detail} pageContract={pageContract} />
      <VaccineBreakdownCard detail={detail} pageContract={pageContract} />
      <AnimalRosterCard rows={animals} nextCursor={nextCursor} loadMoreHref={loadMoreHref} pageContract={pageContract} />
    </div>
  );
}
