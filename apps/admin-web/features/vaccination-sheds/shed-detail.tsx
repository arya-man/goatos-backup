import Link from "next/link";
import { ArrowLeft, CalendarClock, Syringe, UserRound, Warehouse, X } from "lucide-react";
import {
  getGoatPassport,
  getVaccinationShedAnimals,
  getVaccinationShedDetail,
} from "@/lib/api/server";
import type { GoatPassportResponse } from "@/lib/api/server";
import type {
  VaccinationCapacityStatus,
  VaccinationOperationsCounts,
  VaccinationPlannedSession,
  VaccinationShedAnimalRow,
  VaccinationShedDetail,
  VaccinationShedVaccineRow,
} from "@/lib/api/vaccination-sheds";
import { Tag, InfoTooltip, ClipText, type Tone } from "@/components/ui-primitives";
import { dash, fmtDate } from "@/lib/format";
import { copy, optionLabel, optionTone, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseScope, scopeHref } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { HerdPassportVaccinationBlock } from "@/features/counts";

const ANIMAL_PAGE_SIZE = 100;
type GoatPassport = GoatPassportResponse["goat"];
type ShedStatus = VaccinationShedDetail["status"];

// Nonzero obligation counts to surface in the vaccine-breakdown Counts cell. Labels come from the
// work_state_filter_chips contract group (never hardcoded); `accepted` maps to the "completed" chip.
const COUNT_CHIPS: { field: keyof VaccinationOperationsCounts; key: string }[] = [
  { field: "overdue", key: "overdue" },
  { field: "due", key: "due" },
  { field: "proofPending", key: "proof_pending" },
  { field: "rejected", key: "rejected" },
  { field: "accepted", key: "completed" },
];

function shedStatusLabel(pageContract: AdminUiPageContract, status: ShedStatus): string {
  if (status === "scheduled") return copy(pageContract, "status.scheduled_drive");
  if (status === "on_track") return copy(pageContract, "status.no_work_due");
  return optionLabel(pageContract, "shed_status_chips", status);
}

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

function withHash(href: string, hash: string): string {
  return href.includes("#") ? href : `${href}#${hash}`;
}

function passportStatusTone(value: string | null | undefined, kind: "lifecycle" | "health" | "breeding"): Tone {
  const v = String(value ?? "").toLowerCase();
  if (!v) return "mut";
  if (kind === "health") {
    if (["healthy", "normal", "ok"].includes(v)) return "ok";
    if (["sick", "critical", "dead"].includes(v)) return "dng";
    if (v.includes("treatment") || v.includes("watch") || v.includes("quarantine")) return "warn";
    return "info";
  }
  if (kind === "breeding") {
    if (v.includes("pregnant") || v.includes("lactating") || v.includes("ai")) return "info";
    if (v.includes("open") || v.includes("none")) return "mut";
    return "ok";
  }
  if (["alive", "active"].includes(v)) return "ok";
  if (["sold", "died", "culled", "lost", "inactive"].includes(v)) return "dng";
  return "mut";
}

function humanStatus(value: string | null | undefined): string {
  return String(value ?? "")
    .replace(/[_-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b\w/g, (m) => m.toUpperCase());
}

function animalVaccinationWorkLabel(pageContract: AdminUiPageContract, status: string | undefined): string {
  const key = String(status ?? "no_record") === "done" ? "up_to_date" : String(status ?? "no_record");
  try {
    return copy(pageContract, `animals.status.${key}`);
  } catch {
    return humanStatus(key);
  }
}

function animalVaccinationWorkTone(status: string | undefined): Tone {
  const key = String(status ?? "no_record") === "done" ? "up_to_date" : String(status ?? "no_record");
  if (["dead", "culled", "lost", "inactive", "due", "missed"].includes(key)) return "dng";
  if (["sick", "under_treatment", "quarantine", "icu", "deferred"].includes(key)) return "warn";
  if (key === "scheduled") return "info";
  if (key === "up_to_date") return "ok";
  return "mut";
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
  passportHref,
  pageContract,
}: {
  rows: VaccinationShedAnimalRow[];
  nextCursor: string | null;
  loadMoreHref: string | null;
  passportHref: (goatId: string) => string;
  pageContract: AdminUiPageContract;
}) {
  const cols = [
    copy(pageContract, "animals.column.display_id"),
    copy(pageContract, "animals.column.tag_1"),
    copy(pageContract, "animals.column.tag_2"),
    copy(pageContract, "animals.column.breed"),
    copy(pageContract, "animals.column.sex"),
    copy(pageContract, "animals.column.age"),
    copy(pageContract, "animals.column.lifecycle"),
    copy(pageContract, "animals.column.health"),
    copy(pageContract, "animals.column.last_vax_date"),
    copy(pageContract, "animals.column.next_vax_date"),
    copy(pageContract, "animals.column.vax_work"),
  ];
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
            <table className="shed-animal-roster-table">
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
                      <Link href={passportHref(a.goatId)} replace scroll={false} className="gid">
                        {a.displayId}
                      </Link>
                    </td>
                    <td className="muted">{a.tag1 ?? copy(pageContract, "label.placeholder")}</td>
                    <td className="muted">{a.tag2 ?? copy(pageContract, "label.placeholder")}</td>
                    <td className="muted">{a.breed ?? copy(pageContract, "label.placeholder")}</td>
                    <td className="muted">{a.sex}</td>
                    <td className="muted">{a.age ?? copy(pageContract, "label.placeholder")}</td>
                    <td>
                      {a.lifecycleStatus ? (
                        <Tag tone={passportStatusTone(a.lifecycleStatus, "lifecycle")}>{humanStatus(a.lifecycleStatus)}</Tag>
                      ) : (
                        <span className="muted">{copy(pageContract, "label.placeholder")}</span>
                      )}
                    </td>
                    <td>
                      {a.healthStatus ? (
                        <Tag tone={passportStatusTone(a.healthStatus, "health")}>{humanStatus(a.healthStatus)}</Tag>
                      ) : (
                        <span className="muted">{copy(pageContract, "label.placeholder")}</span>
                      )}
                    </td>
                    <td className="muted">{a.lastDose ? fmtDate(a.lastDose) : copy(pageContract, "label.placeholder")}</td>
                    <td className="muted">{a.nextDue ? fmtDate(a.nextDue) : copy(pageContract, "label.placeholder")}</td>
                    <td>
                      <Tag tone={animalVaccinationWorkTone(a.status)}>{animalVaccinationWorkLabel(pageContract, a.status)}</Tag>
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

function ShedAnimalPassportDrawer({
  goat,
  goatId,
  error,
  closeHref,
  fullPassportHref,
  pageContract,
}: {
  goat: GoatPassport | null;
  goatId: string;
  error: string | null;
  closeHref: string;
  fullPassportHref: string;
  pageContract: AdminUiPageContract;
}) {
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.passport.close_label")} scroll={false} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.passport.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)", fontWeight: 800 }}>
            G
          </span>
          <div>
            <div className="mt">{goat?.display_id ?? goatId.slice(0, 8)}</div>
            <h2>{copy(pageContract, "drawer.passport.aria")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.passport.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          {goat ? (
            <>
              <div className="helpgrid" style={{ marginBottom: 12 }}>
                <div className="hk">{copy(pageContract, "label.display_id")}</div>
                <div><span className="gid">{goat.display_id}</span></div>
                <div className="hk">{copy(pageContract, "label.tag_1")}</div>
                <div className="mono">{dash(goat.summary.animal_identifier_1)}</div>
                <div className="hk">{copy(pageContract, "label.tag_2")}</div>
                <div className="mono">{dash(goat.summary.animal_identifier_2)}</div>
              </div>
              <div className="helpgrid">
                <div className="hk">{copy(pageContract, "label.location")}</div>
                <div>{dash(goat.summary.location_path.display)}</div>
                <div className="hk">{copy(pageContract, "label.breed_sex")}</div>
                <div>{dash([goat.summary.breed, goat.summary.sex].filter(Boolean).join(" / "))}</div>
                <div className="hk">{copy(pageContract, "label.lifecycle")}</div>
                <div>
                  <Tag tone={passportStatusTone(goat.summary.lifecycle_status, "lifecycle")}>{dash(goat.summary.lifecycle_status)}</Tag>
                </div>
                <div className="hk">{copy(pageContract, "label.health")}</div>
                <div>
                  <Tag tone={passportStatusTone(goat.summary.health_status, "health")}>{dash(goat.summary.health_status)}</Tag>
                </div>
                <div className="hk">{copy(pageContract, "label.reproductive")}</div>
                <div>
                  <Tag tone={passportStatusTone(goat.summary.reproductive_status, "breeding")}>{dash(goat.summary.reproductive_status)}</Tag>
                </div>
              </div>
              <HerdPassportVaccinationBlock goatId={goat.goat_id} />
            </>
          ) : (
            <div className="alert">
              <b>{copy(pageContract, "fallback.title")}</b>&nbsp;{error ?? copy(pageContract, "fallback.body")}
            </div>
          )}
        </div>
        <div className="df">
          <Link href={fullPassportHref} className="btn p">
            {copy(pageContract, "action.full_change_history")}
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            {copy(pageContract, "action.close")}
          </Link>
        </div>
      </aside>
    </>
  );
}

// Shed-wise vaccination detail — planned sessions (with capacity), per-vaccine breakdown, and the shed's
// animal roster. Route: /vaccination/execution/sheds/{shedId}. Back returns to the board's exact
// filtered/paginated state via the ?ret param the board attached; falls back to /vaccination#sheds.
export async function VaccinationShedDetailPage({
  shedId,
  searchParams,
  pageContract,
  passportPageContract,
}: {
  shedId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
  passportPageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const ret = one(sp, "ret");
  const animalsCursor = one(sp, "animals_cursor");
  const selectedGoatId = one(sp, "goat_passport");

  const [detailResult, animalsResult, passportResult] = await Promise.all([
    getVaccinationShedDetail(shedId),
    getVaccinationShedAnimals(shedId, { cursor: animalsCursor, limit: ANIMAL_PAGE_SIZE }),
    selectedGoatId ? getGoatPassport(selectedGoatId) : Promise.resolve(null),
  ]);

  const fallbackBack = ret && ret.startsWith("/vaccination") ? ret : `${scopeHref("/vaccination", scope)}#sheds`;
  if (!detailResult.ok) {
    return <NotFoundOrError shedId={shedId} message={detailResult.error.message} backHref={fallbackBack} pageContract={pageContract} />;
  }
  const detail = detailResult.data;
  // Keep generated links in this shed's park scope without throwing a render-time redirect.
  // In dev/prod RSC streaming, NEXT_REDIRECT from this path is surfaced by the app error boundary.
  const currentPath = `/vaccination/execution/sheds/${encodeURIComponent(shedId)}`;
  const detailScope = detail.parkId ? ({ mode: "park" as const, park: detail.parkId }) : {};
  const detailHref = (extra: Record<string, string | undefined> = {}) => scopeHref(currentPath, scope, detailScope, extra);
  const backHref = ret && ret.startsWith("/vaccination") ? ret : `${scopeHref("/vaccination", scope, { mode: "park", park: detail.parkId })}#sheds`;

  const animals: VaccinationShedAnimalRow[] = animalsResult.ok ? animalsResult.data.rows : [];
  const nextCursor = animalsResult.ok ? animalsResult.data.nextCursor ?? null : null;
  const loadMoreHref = nextCursor ? `${detailHref({ ret, animals_cursor: nextCursor })}#animals` : null;
  const passportHref = (goatId: string) => withHash(detailHref({ ret, animals_cursor: animalsCursor, goat_passport: goatId }), "animals");
  const closePassportHref = withHash(detailHref({ ret, animals_cursor: animalsCursor }), "animals");
  const selectedGoat = passportResult && passportResult.ok ? passportResult.data.goat : null;
  const selectedGoatError = passportResult && !passportResult.ok ? passportResult.error.message : null;

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
          <Tag tone={statusTone}>{shedStatusLabel(pageContract, detail.status)}</Tag>
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
      <AnimalRosterCard rows={animals} nextCursor={nextCursor} loadMoreHref={loadMoreHref} passportHref={passportHref} pageContract={pageContract} />
      {selectedGoatId ? (
        <ShedAnimalPassportDrawer
          goat={selectedGoat}
          goatId={selectedGoatId}
          error={selectedGoatError}
          closeHref={closePassportHref}
          fullPassportHref={`/goats/${encodeURIComponent(selectedGoatId)}`}
          pageContract={passportPageContract}
        />
      ) : null}
    </div>
  );
}
