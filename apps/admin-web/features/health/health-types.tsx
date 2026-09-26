import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { copy, optionalCopy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getHealthConfigDiagnosisTypes, type HealthDiagnosisRouting } from "@/lib/api/server";

import {
  AddStageToType,
  DiagnosisTypeControls,
  MapStageButton,
  RemoveStageChip,
} from "./health-types-controls";
import Alert from "@mui/material/Alert";

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
        <Alert severity="error" style={{ marginBottom: 16 }}><div>{copy(pageContract, "action.error_backend")}</div>
        </Alert>
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
            <Table className="feed-table" aria-label={copy(pageContract, "table.gaps.aria")}>
              <TableHead>
                <TableRow>
                  {gapCols.map((c) => <TableCell component="th" key={c}>{c}</TableCell>)}
                  <TableCell component="th">{copy(pageContract, "action.map_stage")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {gaps.map((g) => (
                  <TableRow key={`${g.age_band}/${g.stage_code}`}>
                    <TableCell>{g.stage_label || g.stage_code}</TableCell>
                    <TableCell>{bandLabel(g.age_band)}</TableCell>
                    <TableCell><b>{g.live_animals}</b></TableCell>
                    <TableCell>
                      <MapStageButton
                        pageContract={pageContract}
                        ageBand={g.age_band}
                        stageCode={g.stage_code}
                        stageLabel={g.stage_label || g.stage_code}
                        types={activeTypes}
                        enabled={mayWrite}
                        disabledReason={writeDisabledReason}
                      />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
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
          <Table className="feed-table" aria-label={copy(pageContract, "table.types.aria")}>
            <TableHead>
              <TableRow>
                {typeCols.map((c) => <TableCell component="th" key={c}>{c}</TableCell>)}
                <TableCell component="th">{copy(pageContract, "action.edit_type")}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {types.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={typeCols.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>
                      {copy(pageContract, "empty.types")}
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                types.map((t) => (
                  <TableRow key={t.type_key}>
                    <TableCell>
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
                    </TableCell>
                    <TableCell className="small muted">{t.type_key}</TableCell>
                    <TableCell>{t.status === "active" ? copy(pageContract, "label.register_live") : copy(pageContract, "label.retired")}</TableCell>
                    <TableCell>{t.route_count}</TableCell>
                    <TableCell>
                      {t.has_published_register ? (
                        copy(pageContract, "label.register_live")
                      ) : (
                        // A type with no rules yet is a REAL state -- it is what a farm has
                        // between creating the type and writing its register -- so it says so
                        // rather than leaving a blank for the reader to interpret.
                        <span className="small muted">{copy(pageContract, "note.no_register_yet")}</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <DiagnosisTypeControls
                        pageContract={pageContract}
                        mode="edit"
                        type={t}
                        enabled={mayWrite}
                        disabledReason={writeDisabledReason}
                      />
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </section>

      {/* ------------------------------------------------------- the routing, CATEGORY-FIRST.
          Maintainer decision 2026-09-23: the categories are fixed, and which stages come under
          each one is configured. This used to be one row per STAGE, which made the reader
          assemble each category in their head -- four scattered rows to see what the fattening
          type covers. It reads the way the farm says it now: the type, then its stage tags. */}
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.routes.by_type")}</h3>
          <span className="small muted">{copy(pageContract, "section.routes.caption")}</span>
        </div>
        <p className="small muted" style={{ margin: "0 14px 10px", lineHeight: 1.6 }}>
          {copy(pageContract, "section.routes.note")}
        </p>
        <div className="bd" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          {activeTypes.map((t) => {
            const mine = routes.filter((r) => r.type_key === t.type_key);
            const animals = mine.reduce((sum, r) => sum + r.live_animals, 0);
            return (
              <div
                key={t.type_key}
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  alignItems: "center",
                  gap: 8,
                  paddingBottom: 12,
                  borderBottom: "1px solid var(--line)",
                }}
              >
                <div style={{ minWidth: 160 }}>
                  <b>{t.label}</b>
                  {/* The number that makes the panel worth opening: how many animals this whole
                      category actually covers right now. */}
                  <div className="small muted">
                    {animals} · {copy(pageContract, "label.live_animals")}
                  </div>
                </div>

                {mine.length === 0 ? (
                  <span className="small muted">{copy(pageContract, "label.no_stages_yet")}</span>
                ) : (
                  mine.map((r) =>
                    r.is_wildcard ? (
                      // The catch-all is a chip like the others and is NOT one: removing it takes
                      // every unnamed stage in the band out of diagnosis, so it says what it is.
                      <span
                        key={`${r.age_band}/${r.stage_code}`}
                        className="chip"
                        title={copy(pageContract, "note.wildcard_route")}
                      >
                        {copy(pageContract, "label.stage_every")} ({r.live_animals})
                      </span>
                    ) : (
                      <RemoveStageChip
                        key={`${r.age_band}/${r.stage_code}`}
                        pageContract={pageContract}
                        ageBand={r.age_band}
                        stageCode={r.stage_code}
                        label={`${r.stage_label || r.stage_code} (${r.live_animals})`}
                        enabled={mayWrite}
                        disabledReason={writeDisabledReason}
                      />
                    ),
                  )
                )}

                <div className="sp" style={{ flex: 1 }} />
                <AddStageToType
                  pageContract={pageContract}
                  typeKey={t.type_key}
                  stages={stages}
                  enabled={mayWrite}
                  disabledReason={writeDisabledReason}
                />
              </div>
            );
          })}
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
