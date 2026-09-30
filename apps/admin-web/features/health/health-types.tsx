import Table from "@mui/material/Table";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import CardHeader from "@mui/material/CardHeader";
import Chip from "@mui/material/Chip";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
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
import { Scrollbar } from "@/components/minimal/scrollbar";
import { STICKY_FIRST_COLUMN_SX, TableHeadCustom } from "@/components/app/table";

/** A section's standing note under its CardHeader (template body2 secondary, card gutter). */
function SectionNote({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="body2" sx={{ color: "text.secondary", px: 3, pt: 1, pb: 1.5 }}>
      {children}
    </Typography>
  );
}

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
        <Alert severity="error" sx={{ mb: 2 }}><div>{copy(pageContract, "action.error_backend")}</div>
        </Alert>
      ) : null}

      {/* ---------------------------------------------------------------- the gaps, FIRST.
          Above the tables that explain them, because this is the only surface that can show a
          stage holding animals nobody can observe, and a reader who scrolls past it has missed
          the thing the screen exists for. It renders only when there is something to say. */}
      {gaps.length > 0 ? (
        <Card component="section" sx={{ mb: 2, border: 1, borderColor: "error.main" }}>
          <CardHeader
            title={copy(pageContract, "section.gaps.title")}
            subheader={copy(pageContract, "section.gaps.caption")}
          />
          <SectionNote>{copy(pageContract, "section.gaps.note")}</SectionNote>
          <Scrollbar tabIndex={0} role="group" aria-label={copy(pageContract, "table.gaps.aria")}>
            <Table aria-label={copy(pageContract, "table.gaps.aria")} sx={{ minWidth: 640, ...STICKY_FIRST_COLUMN_SX }}>
              <TableHeadCustom
                headCells={[
                  ...gapCols.map((c, index) => ({ id: `c${index}`, label: c })),
                  { id: "map", label: copy(pageContract, "action.map_stage") },
                ]}
              />
              <TableBody>
                {gaps.map((g) => (
                  <TableRow key={`${g.age_band}/${g.stage_code}`}>
                    <TableCell>{g.stage_label || g.stage_code}</TableCell>
                    <TableCell>{bandLabel(g.age_band)}</TableCell>
                    <TableCell sx={{ typography: "subtitle2" }}>{g.live_animals}</TableCell>
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
          </Scrollbar>
        </Card>
      ) : null}

      {/* ---------------------------------------------------------------------- the types */}
      <Card component="section" sx={{ mb: 2 }}>
        <CardHeader
          title={copy(pageContract, "section.types.title")}
          subheader={copy(pageContract, "section.types.caption")}
          action={
            <DiagnosisTypeControls
              pageContract={pageContract}
              mode="create"
              enabled={mayWrite}
              disabledReason={writeDisabledReason}
            />
          }
          sx={{ flexWrap: "wrap", gap: 1 }}
        />
        <SectionNote>{copy(pageContract, "section.types.note")}</SectionNote>
        <Scrollbar tabIndex={0} role="group" aria-label={copy(pageContract, "table.types.aria")}>
          <Table aria-label={copy(pageContract, "table.types.aria")} sx={{ minWidth: 720, ...STICKY_FIRST_COLUMN_SX }}>
            <TableHeadCustom
              headCells={[
                ...typeCols.map((c, index) => ({ id: `c${index}`, label: c })),
                { id: "edit", label: copy(pageContract, "action.edit_type") },
              ]}
            />
            <TableBody>
              {types.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={typeCols.length + 1}>
                    <Typography variant="body2" component="div" sx={{ color: "text.secondary", py: 2, px: 0.5, textAlign: "center" }}>
                      {copy(pageContract, "empty.types")}
                    </Typography>
                  </TableCell>
                </TableRow>
              ) : (
                types.map((t) => (
                  <TableRow key={t.type_key}>
                    <TableCell>
                      <Typography variant="subtitle2" component="span">{t.label}</Typography>
                      {t.is_builtin ? (
                        <Chip size="small" variant="soft" label={copy(pageContract, "label.builtin")} sx={{ ml: 1 }} />
                      ) : null}
                      {t.status === "retired" ? (
                        <Chip size="small" variant="soft" label={copy(pageContract, "label.retired")} sx={{ ml: 1 }} />
                      ) : null}
                    </TableCell>
                    <TableCell sx={{ typography: "body2", color: "text.secondary" }}>{t.type_key}</TableCell>
                    <TableCell>{t.status === "active" ? copy(pageContract, "label.register_live") : copy(pageContract, "label.retired")}</TableCell>
                    <TableCell>{t.route_count}</TableCell>
                    <TableCell>
                      {t.has_published_register ? (
                        copy(pageContract, "label.register_live")
                      ) : (
                        // A type with no rules yet is a REAL state -- it is what a farm has
                        // between creating the type and writing its register -- so it says so
                        // rather than leaving a blank for the reader to interpret.
                        <Typography variant="body2" component="span" sx={{ color: "text.secondary" }}>{copy(pageContract, "note.no_register_yet")}</Typography>
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
        </Scrollbar>
      </Card>

      {/* ------------------------------------------------------- the routing, CATEGORY-FIRST.
          Maintainer decision 2026-09-23: the categories are fixed, and which stages come under
          each one is configured. This used to be one row per STAGE, which made the reader
          assemble each category in their head -- four scattered rows to see what the fattening
          type covers. It reads the way the farm says it now: the type, then its stage tags. */}
      <Card component="section" sx={{ mb: 2 }}>
        <CardHeader
          title={copy(pageContract, "section.routes.by_type")}
          subheader={copy(pageContract, "section.routes.caption")}
        />
        <SectionNote>{copy(pageContract, "section.routes.note")}</SectionNote>
        <CardContent sx={{ pt: 0 }}>
          <Stack spacing={1.75}>
            {activeTypes.map((t) => {
              const mine = routes.filter((r) => r.type_key === t.type_key);
              const animals = mine.reduce((sum, r) => sum + r.live_animals, 0);
              return (
                <Stack
                  key={t.type_key}
                  direction="row"
                  spacing={1}
                  useFlexGap
                  sx={{
                    flexWrap: "wrap",
                    alignItems: "center",
                    pb: 1.5,
                    borderBottom: 1,
                    borderColor: "divider",
                  }}
                >
                  <Box sx={{ minWidth: 160 }}>
                    <Typography variant="subtitle2">{t.label}</Typography>
                    {/* The number that makes the panel worth opening: how many animals this whole
                        category actually covers right now. */}
                    <Typography variant="body2" sx={{ color: "text.secondary" }}>
                      {animals} · {copy(pageContract, "label.live_animals")}
                    </Typography>
                  </Box>

                  {mine.length === 0 ? (
                    <Typography variant="body2" component="span" sx={{ color: "text.secondary" }}>
                      {copy(pageContract, "label.no_stages_yet")}
                    </Typography>
                  ) : (
                    mine.map((r) =>
                      r.is_wildcard ? (
                        // The catch-all is a chip like the others and is NOT one: removing it takes
                        // every unnamed stage in the band out of diagnosis, so it says what it is.
                        <Chip
                          key={`${r.age_band}/${r.stage_code}`}
                          size="small"
                          variant="soft"
                          title={copy(pageContract, "note.wildcard_route")}
                          label={`${copy(pageContract, "label.stage_every")} (${r.live_animals})`}
                        />
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

                  <Box sx={{ flex: 1 }} />
                  <AddStageToType
                    pageContract={pageContract}
                    typeKey={t.type_key}
                    stages={stages}
                    enabled={mayWrite}
                    disabledReason={writeDisabledReason}
                  />
                </Stack>
              );
            })}
          </Stack>
        </CardContent>
      </Card>

      {/* Clinical placements, reported and explained rather than presented as work. */}
      {placements.length > 0 ? (
        <Typography variant="body2" sx={{ color: "text.secondary", mx: 0.5, mb: 2 }}>
          {copy(pageContract, "label.clinical_placement")}:{" "}
          {placements.map((p) => `${p.stage_label || p.stage_code} (${p.live_animals})`).join(", ")}
        </Typography>
      ) : null}
    </>
  );
}
