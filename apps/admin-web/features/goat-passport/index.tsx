import Table from "@mui/material/Table";
import { DividedStack } from "@/components/app/divided-stack";
import type { ReactNode } from "react";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { BlockSkeleton, ListCardSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { randomUUID } from "node:crypto";
import { redirect } from "next/navigation";
import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import MuiCard from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Divider from "@mui/material/Divider";
import Grid from "@mui/material/Grid";
import ListItemText from "@mui/material/ListItemText";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dateTime, dash, humanizeEnum, joinParts, shortId } from "@/lib/format";
import { PageHeader } from "@/components/app/page-header";
import { Tag, type Tone } from "@/components/ui-primitives";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table";
import { OrderDetailsHistory } from "@/components/minimal/sections/order/order-details-history";
import { PassportTabs } from "./passport-tabs";
import { firstAuthRequiredError, getGoatPassport, getGoatTimeline } from "@/lib/api/server";
import { hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { actionFeedbackCopy, copy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { addIdentifierAction, retireIdentifierAction } from "./actions";
import { PassportConfirmSubmitButton, PassportFormCheckbox, PassportFormSelect } from "./passport-form-controls";
import TextField from "@mui/material/TextField";
import Alert from "@mui/material/Alert";

function identifierTypeLabel(type: string, pageContract: AdminUiPageContract): string {
  if (type === "animal_identifier_1") return copy(pageContract, "label.tag_1");
  if (type === "animal_identifier_2") return copy(pageContract, "label.tag_2");
  return humanizeEnum(type);
}


function lifecycleTone(value: string | null | undefined): Tone {
  const v = String(value ?? "").toLowerCase();
  if (!v) return "mut";
  if (["alive", "active"].includes(v)) return "ok";
  if (["dead", "died", "deceased", "culled"].includes(v)) return "dng";
  if (["sold", "transferred", "merged"].includes(v)) return "info";
  return "mut";
}
function healthTone(value: string | null | undefined): Tone {
  const v = String(value ?? "").toLowerCase();
  if (!v) return "mut";
  if (["healthy", "normal", "ok"].includes(v)) return "ok";
  if (["sick", "critical", "dead"].includes(v)) return "dng";
  if (v.includes("treatment") || v.includes("watch") || v.includes("quarantine")) return "warn";
  return "info";
}

function Notice({ status, actionKey, pageContract }: { status?: string; actionKey?: string; pageContract: AdminUiPageContract }) {
  if (!status) return null;
  if (status === "success") {
    return (
      <Alert severity="success" sx={{ mb: 3 }}>
        {actionFeedbackCopy(pageContract, status, actionKey)}
      </Alert>
    );
  }
  return (
    <Alert severity="error" sx={{ mb: 3 }}>
      {actionFeedbackCopy(pageContract, status, actionKey)}
    </Alert>
  );
}

function FormSelect({
  name,
  label,
  options,
  required,
  defaultValue,
  emptyLabel,
}: {
  name: string;
  label: string;
  options: AdminUiOption[];
  required?: boolean;
  defaultValue?: string;
  emptyLabel?: string;
}) {
  return (
    <PassportFormSelect
      name={name}
      label={label}
      required={required}
      defaultValue={defaultValue}
      emptyLabel={emptyLabel}
      options={options.map((option) => ({ value: option.key, label: option.label }))}
    />
  );
}

function FormField({
  name,
  label,
  required,
  defaultValue,
  placeholder,
}: {
  name: string;
  label: string;
  required?: boolean;
  defaultValue?: string;
  placeholder?: string;
}) {
  return (
    <TextField
      fullWidth
      name={name}
      label={label}
      defaultValue={defaultValue}
      placeholder={placeholder}
      slotProps={{ inputLabel: { shrink: true }, htmlInput: { required } }}
    />
  );
}

// Template UserProfileView tabs (sections/user/view/user-profile-view.tsx): tab param drives
// which body renders below the profile-cover Card. Same shape, different sections.
const TAB_PARAM = "tab";
type PassportTab = "" | "identifiers" | "evidence" | "history";
const ALL_TABS: PassportTab[] = ["", "identifiers", "evidence", "history"];
function normalizeTab(value: string | undefined): PassportTab {
  const v = (value ?? "") as PassportTab;
  return ALL_TABS.includes(v) ? v : "";
}
function tabHref(pathname: string, sp: RouteSearchParams, tab: PassportTab): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    if (key === TAB_PARAM) continue;
    if (Array.isArray(value)) {
      for (const item of value) if (item) next.append(key, item);
    } else if (value) next.set(key, value);
  }
  if (tab) next.set(TAB_PARAM, tab);
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

export async function GoatPassportPage({
  goatId,
  searchParams = {},
  pageContract,
  vaccination,
}: {
  goatId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
  /** The goat's vaccination passport card (streamed by the route). Right column of the summary tab, full width below the other tabs. */
  vaccination?: React.ReactNode;
}) {
  const [result, timeline] = await Promise.all([getGoatPassport(goatId), getGoatTimeline({ goatId, limit: 20 })]);
  const returnTo = hrefWithoutAction(`/goats/${encodeURIComponent(goatId)}`, searchParams);
  const actionStatus = one(searchParams, "action_status");
  const actionKey = one(searchParams, "action_key");
  const authError = firstAuthRequiredError(result, timeline);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  if (!result.ok) {
    return (
      <Box className="screen on">
        <PageHeader title={pageContract.title || copy(pageContract, "fallback.title")} crumbs={[{ label: copy(pageContract, "fallback.title") }]} />
        <Alert severity="error">
          <b>{result.error.code}</b>
          <Box sx={{ mt: 0.5, typography: "body2" }}>{result.error.message}</Box>
        </Alert>
      </Box>
    );
  }

  const { goat, warnings } = result.data;
  const allWarnings = [...warnings, ...goat.summary.warnings];

  const lifecycle = goat.summary.lifecycle_status;
  const health = goat.summary.health_status;
  const selectedTab = normalizeTab(one(searchParams, TAB_PARAM));
  const passportPath = `/goats/${encodeURIComponent(goatId)}`;
  const secondaryLine = joinParts([goat.summary.breed, humanizeEnum(goat.summary.sex)]);
  const placeholder = dash(null);
  const mono = { fontFamily: "monospace" } as const;

  const tabsRow = (
    <PassportTabs
      selectedTab={selectedTab}
      ariaLabel={copy(pageContract, "fallback.title")}
      tabs={[
        { value: "", href: tabHref(passportPath, searchParams, ""), label: copy(pageContract, "section.summary.title"), icon: "solar:user-id-bold" },
        { value: "identifiers", href: tabHref(passportPath, searchParams, "identifiers"), label: copy(pageContract, "section.identifiers.title"), icon: "solar:tag-horizontal-bold-duotone" },
        { value: "evidence", href: tabHref(passportPath, searchParams, "evidence"), label: copy(pageContract, "section.evidence.title"), icon: "solar:file-text-bold" },
        { value: "history", href: tabHref(passportPath, searchParams, "history"), label: copy(pageContract, "section.timeline.title"), icon: "solar:clock-circle-bold" },
      ]}
    />
  );

  // Template account general (sections/account/account-general.tsx), left column: the centred
  // profile Card (pt 10 / pb 5, 144px avatar in the dashed upload ring, caption, status row).
  const profileCard = (
    <MuiCard sx={{ pt: 10, pb: 5, px: 3, textAlign: "center" }}>
      <Box sx={{ p: 1, mx: "auto", width: 144, height: 144, borderRadius: "50%", borderWidth: 1, borderStyle: "dashed", borderColor: "divider" }}>
        <Avatar alt={goat.display_id} sx={{ width: 1, height: 1, bgcolor: "primary.lighter", color: "primary.darker" }}>
          <Iconify icon="solar:user-id-bold" width={56} />
        </Avatar>
      </Box>
      <Typography variant="h6" sx={{ mt: 3 }}>
        {goat.display_id}
      </Typography>
      <Typography variant="caption" component="div" sx={{ mt: 1, color: "text.disabled" }}>
        {secondaryLine || placeholder}
        <br />
        {dash(goat.summary.location_path.operational_location_display)}
      </Typography>
      <Stack direction="row" spacing={1} sx={{ mt: 5, justifyContent: "center", flexWrap: "wrap" }}>
        <Tag tone={lifecycleTone(lifecycle)}>{humanizeEnum(lifecycle)}</Tag>
        {health ? <Tag tone={healthTone(health)}>{humanizeEnum(health)}</Tag> : null}
      </Stack>
      {goat.merged_into_goat_id ? (
        <Typography variant="caption" component="div" sx={{ mt: 3, color: "text.secondary" }}>
          {copy(pageContract, "label.merged_into")} <Box component="span" sx={mono}>{shortId(goat.merged_into_goat_id)}</Box>
        </Typography>
      ) : null}
    </MuiCard>
  );

  // Right column: the account form Card (p 3, two-column field grid) with the passport facts as
  // read-only outlined fields, so the page reads exactly like the template form.
  const readOnly = (key: string, label: string, value: string | null | undefined, opts: { full?: boolean; mono?: boolean } = {}) => (
    <TextField
      key={key}
      label={label}
      value={value ? value : placeholder}
      fullWidth
      sx={opts.full ? { gridColumn: "1 / -1" } : undefined}
      slotProps={{ inputLabel: { shrink: true }, htmlInput: { readOnly: true, "aria-readonly": true, style: opts.mono ? mono : undefined } }}
    />
  );
  const summaryCard = (
    <MuiCard aria-label={copy(pageContract, "section.summary.title")} sx={{ p: 3 }}>
      <Box sx={{ rowGap: 3, columnGap: 2, display: "grid", gridTemplateColumns: { xs: "repeat(1, 1fr)", sm: "repeat(2, 1fr)" } }}>
        {readOnly("display", copy(pageContract, "label.display_id"), goat.display_id)}
        {readOnly("breed", copy(pageContract, "label.breed_sex"), secondaryLine)}
        {readOnly("tag1", copy(pageContract, "label.tag_1"), goat.summary.animal_identifier_1, { mono: true })}
        {readOnly("tag2", copy(pageContract, "label.tag_2"), goat.summary.animal_identifier_2, { mono: true })}
        {readOnly("location", copy(pageContract, "label.location"), goat.summary.location_path.operational_location_display, { full: true })}
        {readOnly("lifecycle", copy(pageContract, "label.lifecycle"), lifecycle ? humanizeEnum(lifecycle) : null)}
        {readOnly("health", copy(pageContract, "label.health"), health ? humanizeEnum(health) : null)}
        {readOnly("repro", copy(pageContract, "label.reproductive"), goat.summary.reproductive_status ? humanizeEnum(goat.summary.reproductive_status) : null)}
        {readOnly("cohort", copy(pageContract, "label.growth_cohort"), goat.summary.growth_cohort_tag)}
        {readOnly("management", copy(pageContract, "label.management"), goat.summary.management_stage)}
      </Box>
    </MuiCard>
  );

  const warningsCard =
    allWarnings.length > 0 ? (
      <MuiCard>
        <CardHeader
          title={copy(pageContract, "section.warnings.title")}
          action={
            <Label variant="soft" color="warning">
              {allWarnings.length}
            </Label>
          }
        />
        <Stack spacing={2} sx={{ p: 3 }}>
          {allWarnings.map((warning, index) => (
            <Alert key={`${warning.code}-${index}`} severity="warning">
              <b>{warning.code}</b>
              <Box sx={{ typography: "body2" }}>{warning.message}</Box>
              {warning.original_goat_id || warning.redirect_goat_id ? (
                <Box sx={{ ...mono, mt: 0.5, typography: "caption" }}>
                  {shortId(warning.original_goat_id)} -&gt; {shortId(warning.redirect_goat_id)}
                </Box>
              ) : null}
            </Alert>
          ))}
        </Stack>
      </MuiCard>
    ) : null;

  const identifierHead = [
    copy(pageContract, "label.type"),
    copy(pageContract, "label.value"),
    copy(pageContract, "label.scope"),
    copy(pageContract, "label.status"),
    copy(pageContract, "label.primary"),
    copy(pageContract, "label.valid_from"),
    copy(pageContract, "label.action"),
  ].map((label, index) => ({ id: `c${index}`, label }));

  const identifiersBody = (
    <MuiCard aria-label={copy(pageContract, "section.identifiers.title")}>
      <CardHeader
        title={copy(pageContract, "section.identifiers.title")}
        action={<Label variant="soft">{goat.identifiers.length}</Label>}
        sx={{ mb: 3 }}
      />
      {goat.identifiers.length === 0 ? (
        <Typography variant="body2" sx={{ px: 3, pb: 3, color: "text.secondary" }}>
          {copy(pageContract, "empty.identifiers")}
        </Typography>
      ) : (
        <Scrollbar>
          <Table sx={{ minWidth: 760 }} aria-label={copy(pageContract, "table.identifiers.aria")}>
            <TableHeadCustom headCells={identifierHead} />
            <TableBody>
              {goat.identifiers.map((identifier) => (
                <TableRow hover key={identifier.identifier_id}>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>{identifierTypeLabel(identifier.identifier_type, pageContract)}</TableCell>
                  <TableCell sx={{ ...mono, whiteSpace: "nowrap" }}>{identifier.identifier_value}</TableCell>
                  <TableCell>{humanizeEnum(identifier.scope_key)}</TableCell>
                  <TableCell>
                    <Tag tone={identifier.status === "active" ? "ok" : "mut"}>{humanizeEnum(identifier.status)}</Tag>
                  </TableCell>
                  <TableCell>{humanizeEnum(identifier.is_primary_for_goat ? copy(pageContract, "label.yes") : copy(pageContract, "label.no"))}</TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>{dateTime(identifier.valid_from)}</TableCell>
                  <TableCell>
                    {identifier.status === "active" ? (
                      <form action={retireIdentifierAction}>
                        <input type="hidden" name="goat_id" value={goat.goat_id} />
                        <input type="hidden" name="identifier_id" value={identifier.identifier_id} />
                        <input type="hidden" name="row_version" value={goat.row_version} />
                        <input type="hidden" name="idempotency_key" value={randomUUID()} />
                        <input type="hidden" name="return_to" value={returnTo} />
                        <input type="hidden" name="evidence_type" value="identifier" />
                        <input type="hidden" name="evidence_id" value={identifier.identifier_id} />
                        <input type="hidden" name="reason" value={copy(pageContract, "reason.retire_identifier")} />
                        <PassportConfirmSubmitButton
                          message={`${copy(pageContract, "confirm.retire_identifier")} ${identifier.identifier_type} ${identifier.identifier_value}`}
                          confirmLabel={copy(pageContract, "action.retire_identifier")}
                          cancelLabel={copy(pageContract, "action.cancel", "Cancel")}
                        >
                          {copy(pageContract, "action.retire_identifier")}
                        </PassportConfirmSubmitButton>
                      </form>
                    ) : (
                      <Box component="span" sx={{ color: "text.disabled" }}>-</Box>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Scrollbar>
      )}

      <Divider sx={{ borderStyle: "dashed" }} />
      {/* Template new/edit form section: subtitle, a responsive grid of outlined fields, submit bottom right. */}
      <Box sx={{ p: 3 }}>
        <form action={addIdentifierAction}>
        <input type="hidden" name="goat_id" value={goat.goat_id} />
        <input type="hidden" name="row_version" value={goat.row_version} />
        <input type="hidden" name="idempotency_key" value={randomUUID()} />
        <input type="hidden" name="return_to" value={returnTo} />
        <Typography variant="subtitle1" sx={{ mb: 3, display: "flex", alignItems: "center", gap: 1 }}>
          <Iconify icon="mingcute:add-line" />
          {copy(pageContract, "section.add_identifier.title")}
        </Typography>
        <Box sx={{ display: "grid", gap: 3, gridTemplateColumns: { xs: "repeat(1, 1fr)", sm: "repeat(2, 1fr)", md: "repeat(3, 1fr)" }, alignItems: "center" }}>
          <FormSelect name="identifier_type" label={copy(pageContract, "field.identifier_type")} options={optionGroup(pageContract, "identifier_types")} required emptyLabel={copy(pageContract, "label.select")} />
          <FormField name="identifier_value" label={copy(pageContract, "field.identifier_value")} required />
          <FormField name="scope_key" label={copy(pageContract, "field.scope_key")} required placeholder={copy(pageContract, "placeholder.scope_key")} />
          <EvidenceFields defaultType="goat" defaultID={goat.goat_id} pageContract={pageContract} />
          <PassportFormCheckbox name="is_primary_for_goat" label={copy(pageContract, "label.primary")} />
        </Box>
        <Box sx={{ mt: 3, display: "flex", justifyContent: "flex-end" }}>
          <Button type="submit" variant="contained" color="primary">
            {copy(pageContract, "action.add_identifier")}
          </Button>
        </Box>
        </form>
      </Box>
    </MuiCard>
  );

  // Template list rows (ProfileFollowers / file list anatomy): rounded avatar, primary + secondary text.
  const evidenceBody = (
    <MuiCard aria-label={copy(pageContract, "section.evidence.title")}>
      <CardHeader title={copy(pageContract, "section.evidence.title")} action={<Label variant="soft">{goat.evidence_refs.length}</Label>} />
      <DividedStack flexItem={false} spacing={2.5} sx={{ p: 3 }}>
        {goat.evidence_refs.length === 0 ? (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "empty.evidence")}
          </Typography>
        ) : (
          goat.evidence_refs.map((evidence) => (
            <Box key={`${evidence.evidence_type}-${evidence.evidence_id}`} sx={{ display: "flex", alignItems: "center", gap: 2, minWidth: 0 }}>
              <Avatar variant="rounded" sx={{ bgcolor: "background.neutral", color: "primary.main", width: 48, height: 48 }}>
                <Iconify icon="solar:file-text-bold" width={24} />
              </Avatar>
              <ListItemText
                primary={humanizeEnum(evidence.evidence_type)}
                secondary={
                  <>
                    <Box component="span" sx={{ ...mono, display: "block", overflowWrap: "anywhere" }}>{evidence.evidence_id}</Box>
                    {dash(evidence.description ?? evidence.source_system)}
                  </>
                }
                slotProps={{ primary: { sx: { typography: "subtitle2" } }, secondary: { component: "span", sx: { typography: "body2" } } }}
                sx={{ minWidth: 0 }}
              />
            </Box>
          ))
        )}
      </DividedStack>
    </MuiCard>
  );

  const timelineItems = timeline.ok ? listOrEmpty(timeline.data.items) : [];
  const historyBody = !timeline.ok ? (
    <Alert severity="error">
      <b>{timeline.error.code}</b>
      <Box sx={{ mt: 0.5, typography: "body2" }}>{timeline.error.message}</Box>
    </Alert>
  ) : timelineItems.length === 0 ? (
    <MuiCard aria-label={copy(pageContract, "section.timeline.title")}>
      <CardHeader title={copy(pageContract, "section.timeline.title")} subheader={copy(pageContract, "label.identity_events_table")} />
      <Typography variant="body2" sx={{ p: 3, color: "text.secondary" }}>
        {copy(pageContract, "empty.timeline")}
      </Typography>
    </MuiCard>
  ) : (
    <OrderDetailsHistory
      aria-label={copy(pageContract, "section.timeline.title")}
      title={copy(pageContract, "section.timeline.title")}
      action={<Typography variant="caption" sx={{ color: "text.secondary" }}>{copy(pageContract, "label.identity_events_table")}</Typography>}
      timeline={timelineItems.map((event, index) => ({
        key: event.event_id,
        tone: index === 0 ? "primary" : "grey",
        title: `${humanizeEnum(event.event_type)} · ${humanizeEnum(event.actor_type)}`,
        time: `${copy(pageContract, "label.occurred")} ${dateTime(event.occurred_at)} · ${copy(pageContract, "label.recorded")} ${dateTime(event.recorded_at)}`,
        body: `${copy(pageContract, "label.evidence")} ${event.evidence_refs.length} · ${event.decision_id ? `${copy(pageContract, "label.decision")} ${shortId(event.decision_id)}` : copy(pageContract, "label.no_decision")}`,
      }))}
    />
  );

  return (
    <Box className="screen on">
      <PageHeader
        title={goat.display_id}
        crumbs={[{ label: copy(pageContract, "fallback.title") }, { label: goat.display_id }]}
      />

      <Notice status={actionStatus} actionKey={actionKey} pageContract={pageContract} />

      {tabsRow}

      {/* The tab body (guard: url-keyed-panel): a tab click shows the clicked tab's skeleton at once;
          header and tabs stay on screen. */}
      <UrlSuspense searchParams={searchParams} watch={[TAB_PARAM]} fallback={PASSPORT_TAB_SKELETON[selectedTab]} fallbackBy={{ param: TAB_PARAM, shapes: PASSPORT_TAB_SKELETON }}>
      {selectedTab === "" ? (
        // Template account general: profile Card (md 4) beside the form Card (md 8).
        <Grid container spacing={3}>
          <Grid size={{ xs: 12, md: 4 }}>
            <Stack spacing={3}>
              {profileCard}
              {warningsCard}
            </Stack>
          </Grid>
          <Grid size={{ xs: 12, md: 8 }}>
            <Stack spacing={3}>
              {summaryCard}
              {vaccination}
            </Stack>
          </Grid>
        </Grid>
      ) : (
        <Stack spacing={3}>
          {selectedTab === "identifiers" && identifiersBody}
          {selectedTab === "evidence" && evidenceBody}
          {selectedTab === "history" && historyBody}
          {vaccination}
        </Stack>
      )}
      </UrlSuspense>
    </Box>
  );
}

/** Each passport tab's body skeleton ("" = the summary tab). */
const PASSPORT_TAB_SKELETON: Record<PassportTab, ReactNode> = {
  "": (
    <Grid container spacing={3}>
      <Grid size={{ xs: 12, md: 4 }}>
        <BlockSkeleton height={420} />
      </Grid>
      <Grid size={{ xs: 12, md: 8 }}>
        <StackSkeleton spacing={3}>
          <BlockSkeleton height={400} />
          <TableSkeleton columns={5} rows={4} pager={false} />
        </StackSkeleton>
      </Grid>
    </Grid>
  ),
  identifiers: <PanelSkeleton table={6} tableWidths={["1fr", "1fr", "1fr", "1fr"]} />,
  evidence: <PanelSkeleton table={6} tableWidths={["1fr", "1fr", "1fr", "1fr"]} />,
  history: (
    <StackSkeleton spacing={3}>
      <ListCardSkeleton rows={6} avatar={false} />
    </StackSkeleton>
  ),
};

function EvidenceFields({ defaultType, defaultID, pageContract }: { defaultType: string; defaultID: string; pageContract: AdminUiPageContract }) {
  return (
    <>
      <FormSelect name="evidence_type" label={copy(pageContract, "field.evidence_type")} defaultValue={defaultType} options={optionGroup(pageContract, "evidence_types")} required emptyLabel={copy(pageContract, "label.select")} />
      <FormField name="evidence_id" label={copy(pageContract, "field.evidence_id")} defaultValue={defaultID} required />
      <FormField name="evidence_source_system" label={copy(pageContract, "field.evidence_source")} placeholder={copy(pageContract, "placeholder.optional")} />
    </>
  );
}
