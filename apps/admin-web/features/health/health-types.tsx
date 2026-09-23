import { AlertTriangle } from "lucide-react";

import { copy, optionalCopy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getHealthConfigDiagnosisTypes, type HealthDiagnosisRouting } from "@/lib/api/server";

import { DiagnosisTypeControls, RouteControls, MapStageButton } from "./health-types-controls";

/**
 * WHO IS JUDGED BY WHICH RULEBOOK.
 *
 * The Diagnosis tab authors what a rulebook SAYS; this one authors who it APPLIES TO. They were
 * one screen in an earlier sketch and are two now, because an author working on the adult
 * register's forty questions is not simultaneously deciding that Warmup kids are their own
 * cohort -- and a screen that asks both at once makes the smaller decision easy to miss.
 *
 * Three tables, because they answer three different questions and a reader wants them apart:
 * what types exist, which stages reach them, and -- the one nothing else can show -- which
 * stages reach NOTHING.
 */
export async function HealthTypesSection({
  pageContract,
  mayWrite,
  writeDisabledReason,
}: {
  pageContract: AdminUiPageContract;
  mayWrite: boolean;
  writeDisabledReason: string;
}) {
  const result = await getHealthConfigDiagnosisTypes();
  const data: HealthDiagnosisRouting | null = result.ok ? result.data : null;

  const typeCols = tableLabels(pageContract, "diagnosis-types");
  const routeCols = tableLabels(pageContract, "diagnosis-routes");
  const gapCols = tableLabels(pageContract, "diagnosis-gaps");

  const types = data?.types ?? [];
  const routes = data?.routes ?? [];
  // Clinical placements are reported by the backend but are NOT gaps: they say where an animal is
  // rather than what it eats, so they cannot choose a rulebook. Listing them beside the real gaps
  // would invite exactly the wrong fix -- a medical call made from a placement fact.
  const gaps = (data?.unrouted_stages ?? []).filter((s) => !s.clinical_placement);
  const placements = (data?.unrouted_stages ?? []).filter((s) => s.clinical_placement);

  const activeTypes = types.filter((t) => t.status === "active");
  const stages = data?.stages ?? [];
  const bandLabel = (band: string) =>
    band === "adult" ? copy(pageContract, "label.band.adult") : copy(pageContract, "label.band.kid");

  return (
    <>
      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>{copy(pageContract, "action.error_backend")}</div>
        </div>
      ) : null}

      {/* ---------------------------------------------------------------- the gaps, FIRST.
          Above the tables that explain them, because this is the only surface that can show a
          stage holding animals nobody can observe, and a reader who scrolls past it has missed
          the thing the screen exists for. It renders only when there is something to say. */}
      {gaps.length > 0 ? (
        <section className="card" style={{ marginBottom: 16, borderColor: "var(--danger)" }}>
          <div className="hd">
            <h3>{copy(pageContract, "section.gaps.title")}</h3>
            <span className="small muted">{copy(pageContract, "section.gaps.caption")}</span>
          </div>
          <p className="small muted" style={{ margin: "0 14px 10px", lineHeight: 1.6 }}>
            {copy(pageContract, "section.gaps.note")}
          </p>
          <div className="bd health-scroll" style={{ padding: 0, overflowX: "auto" }} tabIndex={0}
            role="group" aria-label={copy(pageContract, "table.gaps.aria")}>
            <table className="feed-table" aria-label={copy(pageContract, "table.gaps.aria")}>
              <thead>
                <tr>
                  {gapCols.map((c) => <th key={c}>{c}</th>)}
                  <th>{copy(pageContract, "action.map_stage")}</th>
                </tr>
              </thead>
              <tbody>
                {gaps.map((g) => (
                  <tr key={`${g.age_band}/${g.stage_code}`}>
                    <td>{g.stage_label || g.stage_code}</td>
                    <td>{bandLabel(g.age_band)}</td>
                    <td><b>{g.live_animals}</b></td>
                    <td>
                      <MapStageButton
                        pageContract={pageContract}
                        ageBand={g.age_band}
                        stageCode={g.stage_code}
                        stageLabel={g.stage_label || g.stage_code}
                        types={activeTypes}
                        enabled={mayWrite}
                        disabledReason={writeDisabledReason}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}

      {/* ---------------------------------------------------------------------- the types */}
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.types.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.types.caption")}</span>
          <div className="sp" style={{ flex: 1 }} />
          <DiagnosisTypeControls
            pageContract={pageContract}
            mode="create"
            enabled={mayWrite}
            disabledReason={writeDisabledReason}
          />
        </div>
        <p className="small muted" style={{ margin: "0 14px 10px", lineHeight: 1.6 }}>
          {copy(pageContract, "section.types.note")}
        </p>
        <div className="bd health-scroll" style={{ padding: 0, overflowX: "auto" }} tabIndex={0}
          role="group" aria-label={copy(pageContract, "table.types.aria")}>
          <table className="feed-table" aria-label={copy(pageContract, "table.types.aria")}>
            <thead>
              <tr>
                {typeCols.map((c) => <th key={c}>{c}</th>)}
                <th>{copy(pageContract, "action.edit_type")}</th>
              </tr>
            </thead>
            <tbody>
              {types.length === 0 ? (
                <tr>
                  <td colSpan={typeCols.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>
                      {copy(pageContract, "empty.types")}
                    </div>
                  </td>
                </tr>
              ) : (
                types.map((t) => (
                  <tr key={t.type_key}>
                    <td>
                      <b>{t.label}</b>
                      {t.is_builtin ? (
                        <span className="chip" style={{ marginLeft: 8 }}>
                          {copy(pageContract, "label.builtin")}
                        </span>
                      ) : null}
                      {t.status === "retired" ? (
                        <span className="chip" style={{ marginLeft: 8 }}>
                          {copy(pageContract, "label.retired")}
                        </span>
                      ) : null}
                    </td>
                    <td className="small muted">{t.type_key}</td>
                    <td>{t.status === "active" ? copy(pageContract, "label.register_live") : copy(pageContract, "label.retired")}</td>
                    <td>{t.route_count}</td>
                    <td>
                      {t.has_published_register ? (
                        copy(pageContract, "label.register_live")
                      ) : (
                        // A type with no rules yet is a REAL state -- it is what a farm has
                        // between creating the type and writing its register -- so it says so
                        // rather than leaving a blank for the reader to interpret.
                        <span className="small muted">{copy(pageContract, "note.no_register_yet")}</span>
                      )}
                    </td>
                    <td>
                      <DiagnosisTypeControls
                        pageContract={pageContract}
                        mode="edit"
                        type={t}
                        enabled={mayWrite}
                        disabledReason={writeDisabledReason}
                      />
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* --------------------------------------------------------------------- the routing */}
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.routes.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.routes.caption")}</span>
          <div className="sp" style={{ flex: 1 }} />
          <RouteControls
            pageContract={pageContract}
            mode="create"
            types={activeTypes}
            stages={stages}
            enabled={mayWrite}
            disabledReason={writeDisabledReason}
          />
        </div>
        <p className="small muted" style={{ margin: "0 14px 10px", lineHeight: 1.6 }}>
          {copy(pageContract, "section.routes.note")}
        </p>
        <div className="bd health-scroll" style={{ padding: 0, overflowX: "auto" }} tabIndex={0}
          role="group" aria-label={copy(pageContract, "table.routes.aria")}>
          <table className="feed-table" aria-label={copy(pageContract, "table.routes.aria")}>
            <thead>
              <tr>
                {routeCols.map((c) => <th key={c}>{c}</th>)}
                <th>{copy(pageContract, "action.edit_route")}</th>
              </tr>
            </thead>
            <tbody>
              {routes.length === 0 ? (
                <tr>
                  <td colSpan={routeCols.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>
                      {copy(pageContract, "empty.routes")}
                    </div>
                  </td>
                </tr>
              ) : (
                routes.map((r) => (
                  <tr key={`${r.age_band}/${r.stage_code}`}>
                    <td>{bandLabel(r.age_band)}</td>
                    <td>
                      {r.is_wildcard ? (
                        <>
                          <b>{copy(pageContract, "label.every_stage")}</b>
                          {/* The wildcard looks like any other row and is not: removing it makes
                              a whole age band fail-closed. The consequence is said here rather
                              than left to be discovered. */}
                          <div className="small muted" style={{ marginTop: 2, lineHeight: 1.5 }}>
                            {copy(pageContract, "note.wildcard_route")}
                          </div>
                        </>
                      ) : (
                        <>
                          {r.stage_label || r.stage_code}
                          {/* A route whose stage the catalog no longer holds matches nothing. It
                              is shown, and SAID, so it can be removed rather than puzzled over. */}
                          {r.stage_retired ? (
                            <div className="small" style={{ color: "var(--danger)", marginTop: 2 }}>
                              {copy(pageContract, "warn.stage_retired")}
                            </div>
                          ) : null}
                        </>
                      )}
                    </td>
                    <td>{r.type_label}</td>
                    <td className="small muted">{r.sub_stage || "—"}</td>
                    <td><b>{r.live_animals}</b></td>
                    <td>
                      <RouteControls
                        pageContract={pageContract}
                        mode="edit"
                        route={r}
                        types={activeTypes}
                        stages={stages}
                        enabled={mayWrite}
                        disabledReason={writeDisabledReason}
                      />
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* Clinical placements, reported and explained rather than presented as work. */}
      {placements.length > 0 ? (
        <p className="small muted" style={{ margin: "0 4px 16px", lineHeight: 1.6 }}>
          {copy(pageContract, "label.clinical_placement")}:{" "}
          {placements.map((p) => `${p.stage_label || p.stage_code} (${p.live_animals})`).join(", ")}
        </p>
      ) : null}
    </>
  );
}
