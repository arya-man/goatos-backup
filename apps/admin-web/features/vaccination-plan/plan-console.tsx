"use client";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";

import { PageHeader } from "@/components/app/page-header";
import { KpiCard } from "@/components/minimal/widgets";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { LinkButton } from "@/components/minimal/link-button";
import { TableHeadCustom, TablePaginationLinks } from "@/components/minimal/table";

/**
 * Vaccination plan console — the list screen.
 *
 * Template anatomy (course KPI row + table cards + Dialog); no legacy `.vp` markup. Two screens, mirroring what the product
 * already has: this list, and the editor behind "Start a new version". They are
 * deliberately NOT merged — see MOCK-BEHAVIOUR-SPEC.md §2.
 *
 * Copy rules from that spec, enforced here:
 *   - no schema vocabulary on screen (no obligation / rule_dsl / scope / offset)
 *   - version IDs never shown; the label is the identity a person recognises
 *   - no dead text: a card with nothing to say does not render
 */

import { useRouter } from "next/navigation";
import { useCallback, useState, useTransition } from "react";

import type { ProtocolConfigItem } from "@/lib/api/server";
import { fmtDate } from "@/lib/format";

import { discardDraft, readVersionSettings, startNewVersion } from "./plan-actions";
import { describeFirstDoses, describeRepeats, readVaccines, type VaccineGroup } from "./plan-model";
import { personName } from "./version-format";
import { VersionSheet, type VersionSheetData } from "./version-sheet";
import Alert from "@mui/material/Alert";

type Props = {
  versions: ProtocolConfigItem[];
  catalog: VaccineGroup[];
  changeNotes: Record<string, string>;
  loadFailed: boolean;
};

export function VaccinationPlanConsole({ versions, catalog, changeNotes, loadFailed }: Props) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [sheet, setSheet] = useState<VersionSheetData | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [sheetLoading, setSheetLoading] = useState(false);
  const [sheetError, setSheetError] = useState<string | null>(null);
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);

  const live = versions.find((v) => v.status === "published");
  const inPlanCount = catalog.filter((v) => v.inPlan).length;
  const draft = versions.find((v) => v.status === "draft");
  const earlier = versions
    .filter((v) => v.status === "retired")
    .sort((a, b) => (b.version ?? 0) - (a.version ?? 0));

  function onDiscard(draftVersionId: string) {
    setError(null);
    setConfirmingDiscard(false);
    startTransition(async () => {
      const result = await discardDraft(draftVersionId);
      if (!result.ok) setError(result.error);
      router.refresh();
    });
  }

  function onStart() {
    setError(null);
    startTransition(async () => {
      const result = await startNewVersion();
      if (!result.ok) {
        setError(result.error);
        router.refresh();
        return;
      }
      // There is one draft at a time, so this action has exactly one outcome:
      // you are editing it. Whether the draft was just created or already
      // existed, the button lands in the editor rather than returning to a list
      // that then asks you to press a second button to get there.
      if (result.versionId) {
        router.push(`/vaccination/plan/edit?version=${result.versionId}`);
      }
      router.refresh();
    });
  }

  const openVersion = useCallback(async (version: ProtocolConfigItem) => {
    setSheetOpen(true);
    setSheetLoading(true);
    setSheetError(null);
    setSheet(null);
    const result = await readVersionSettings(version.protocol_version_id);
    setSheetLoading(false);
    if (!result.ok) {
      setSheetError(result.error);
      return;
    }
    setSheet({
      label: version.version_label || `V${version.version}`,
      inForce: formatInForceRange(version),
      published: formatDate(version.published_at),
      vaccines: readVaccines(result.ruleDsl, result.rules),
    });
  }, []);

  if (loadFailed) {
    return (
      <Box className="screen on">
        <PageHeader title="Vaccination plan" crumbs={[{ label: "Preventive Care" }, { label: "Vaccination plan" }]} />
        <Alert severity="error">The vaccination plan could not be loaded.</Alert>
      </Box>
    );
  }

  const draftHref = draft ? `/vaccination/plan/edit?version=${draft.protocol_version_id}` : "#";
  const draftLabel = draft ? draft.version_label || `V${draft.version}` : "";
  const liveKpis = live
    ? [
        { key: "since", label: "In force since", value: formatDate(live.effective_from), icon: "solar:calendar-date-bold" as const, tone: "primary" as const },
        { key: "applies", label: "Applies to", value: appliesTo(live), icon: "solar:users-group-rounded-bold" as const, tone: "info" as const },
        { key: "vaccines", label: "Vaccines in the plan", value: `${inPlanCount} of ${catalog.length}`, icon: "solar:medical-kit-bold" as const, tone: "success" as const },
        {
          // "Published by" promises a person; with no author on the record the tile shows only the date.
          key: "published",
          label: personName(live.published_by) ? "Published by" : "Published",
          value: personName(live.published_by) ?? formatDate(live.published_at),
          hint: personName(live.published_by) ? formatDate(live.published_at) : undefined,
          icon: "solar:verified-check-bold" as const,
          tone: "warning" as const,
        },
      ]
    : [];

  return (
    <Box className="screen on">
      <PageHeader
        title="Vaccination plan"
        crumbs={[{ label: "Preventive Care" }, { label: "Vaccination plan" }]}
        actions={
          draft ? (
            <LinkButton href={draftHref} variant="contained" color="primary" startIcon={<Iconify icon="solar:pen-bold" />}>
              Open {draftLabel}
            </LinkButton>
          ) : (
            <Button variant="contained" color="primary" onClick={onStart} disabled={pending} startIcon={<Iconify icon="mingcute:add-line" />}>
              {pending ? "Starting…" : "Start a new version"}
            </Button>
          )
        }
      />

      <Stack spacing={3}>
        {error ? <Alert severity="error">{error}</Alert> : null}

        {live ? (
          <>
            {/* Template overview/course: CourseWidgetSummary tiles for the live version's facts. */}
            <Grid container spacing={3}>
              {liveKpis.map((kpi) => (
                <Grid key={kpi.key} size={{ xs: 12, sm: 6, md: 3 }}>
                  <KpiCard
                    label={kpi.label}
                    // Dates and names are text, not counts: the h4 step keeps DD/MM/YYYY on one line in a quarter-width tile.
                    value={<Box component="span" sx={{ typography: "h4", whiteSpace: "nowrap" }}>{kpi.value}</Box>}
                    hint={kpi.hint} tone={kpi.tone} icon={<Iconify icon={kpi.icon} width={32} />} />
                </Grid>
              ))}
            </Grid>

            {draft ? (
              // Discarding destroys work that cannot be recovered, so it asks first, inline (a native
              // confirm is outside the design system and dismissed by automation).
              <Alert
                severity="warning"
                action={
                  confirmingDiscard ? (
                    <Stack direction="row" spacing={1}>
                      <Button size="small" color="inherit" onClick={() => setConfirmingDiscard(false)}>
                        Keep it
                      </Button>
                      <Button size="small" variant="contained" color="error" disabled={pending} onClick={() => onDiscard(draft.protocol_version_id)}>
                        {pending ? "Discarding…" : "Yes, discard it"}
                      </Button>
                    </Stack>
                  ) : (
                    <Stack direction="row" spacing={1}>
                      <Button size="small" color="inherit" disabled={pending} onClick={() => setConfirmingDiscard(true)}>
                        Discard it
                      </Button>
                      <LinkButton href={draftHref} size="small" variant="contained" color="warning">
                        Open the draft
                      </LinkButton>
                    </Stack>
                  )
                }
              >
                {confirmingDiscard ? (
                  <>
                    <b>Discard {draftLabel}?</b> The draft and everything in it is deleted. This cannot be undone.
                  </>
                ) : (
                  <>
                    <b>A draft is waiting.</b> {draftLabel} — not live yet.
                  </>
                )}
              </Alert>
            ) : null}

            {/* Template table card: CardHeader (eyebrow subheader + status Label), TableHeadCustom. */}
            <Card>
              <CardHeader
                title={live.version_label || `V${live.version}`}
                subheader="Live right now"
                action={
                  <Label variant="soft" color="success" startIcon={<Iconify icon="solar:check-circle-bold" width={16} />}>
                    Published
                  </Label>
                }
                sx={{ mb: 3 }}
              />
              {catalog.length > 0 ? (
                <>
                  <Scrollbar>
                    <Table sx={{ minWidth: 720 }}>
                      <TableHeadCustom
                        headCells={[
                          { id: "vaccine", label: "Vaccine" },
                          { id: "first", label: "First doses" },
                          { id: "repeats", label: "Repeats" },
                          { id: "state", label: "", width: 160 },
                        ]}
                      />
                      <TableBody>
                        {catalog.map((v) => (
                          <TableRow hover key={v.code} sx={v.inPlan ? undefined : { "& td": { color: "text.disabled" } }}>
                            <TableCell sx={{ typography: "subtitle2" }}>{v.name}</TableCell>
                            <TableCell>{v.inPlan ? describeFirstDoses(v.firstDoses) : "—"}</TableCell>
                            <TableCell>{v.inPlan ? describeRepeats(v.repeats) : "—"}</TableCell>
                            <TableCell>
                              <Label variant="soft" color={v.inPlan ? "success" : "default"}>
                                {v.inPlan ? "in the plan" : "not in this plan"}
                              </Label>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </Scrollbar>
                  <TablePaginationLinks
                    page={0}
                    rowsPerPage={catalog.length}
                    count={catalog.length}
                    hideActions
                    rangeLabel={`Page 1 · ${catalog.length} vaccine${catalog.length === 1 ? "" : "s"} on this page`}
                    prevLabel="Previous"
                    nextLabel="Next"
                  />
                </>
              ) : null}
            </Card>
          </>
        ) : null}

        {earlier.length > 0 ? (
          <Card>
            <CardHeader title="Earlier versions" action={<Label variant="soft">{earlier.length}</Label>} sx={{ mb: 3 }} />
            <Scrollbar>
              <Table sx={{ minWidth: 820 }}>
                <TableHeadCustom
                  headCells={[
                    { id: "version", label: "Version" },
                    { id: "inforce", label: "In force" },
                    { id: "published", label: "Published" },
                    { id: "changed", label: "What changed" },
                    { id: "action", label: "", width: 140 },
                  ]}
                />
                <TableBody>
                  {earlier.map((v) => (
                    <TableRow hover key={v.protocol_version_id}>
                      <TableCell sx={{ typography: "subtitle2" }}>{v.version_label || `V${v.version}`}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{formatInForceRange(v)}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>
                        {formatDate(v.published_at)}
                        {personName(v.published_by) ? (
                          <Box component="span" sx={{ display: "block", typography: "caption", color: "text.secondary" }}>
                            {personName(v.published_by)}
                          </Box>
                        ) : null}
                      </TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>{changeNotes[v.protocol_version_id] ?? "—"}</TableCell>
                      <TableCell align="right">
                        <Button size="small" variant="outlined" color="inherit" onClick={() => void openVersion(v)}>
                          View settings
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Scrollbar>
          </Card>
        ) : null}
      </Stack>

      <VersionSheet open={sheetOpen} data={sheet} loading={sheetLoading} error={sheetError} onClose={() => setSheetOpen(false)} />
    </Box>
  );
}

/**
 * "Both parks" / the park's own name.
 *
 * scope_type is the schema's word ("tenant"), and the spec forbids schema
 * vocabulary on screen. A tenant-scoped plan governs every park, which is what
 * the reader needs to know.
 */
function appliesTo(live: ProtocolConfigItem): string {
  if (live.scope_type === "park") return live.scope_label || "One park";
  return "Both parks";
}

/**
 * A publisher's name, or nothing.
 *
 * published_by holds a user id. There is no people lookup on this screen, and a
 * raw UUID on a CEO's screen is worse than no name at all -- the spec forbids
 * showing ids, and "90000000-0000-4000-..." tells the reader strictly less than
 * the date already does.
 */
function formatInForceRange(version: ProtocolConfigItem): string {
  const retiredAt =
    "retired_at" in version && typeof version.retired_at === "string" ? version.retired_at : null;
  const end = version.effective_to ?? retiredAt;
  return `${formatDate(version.effective_from)} – ${formatDate(end)}`;
}


function formatDate(value: string | undefined | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return fmtDate(date.toISOString());
}
