import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Divider from "@mui/material/Divider";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import MuiLink from "@mui/material/Link";
import Link from "@/components/no-prefetch-link";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table";
import { OrderDetailsToolbar } from "@/components/minimal/sections/order/order-details-toolbar";
import { OrderDetailsHistory, type OrderHistoryItem } from "@/components/minimal/sections/order/order-details-history";
import { OrderDetailsCustomer } from "@/components/minimal/sections/order/order-details-customer";
import { OrderDetailsDelivery } from "@/components/minimal/sections/order/order-details-delivery";
import { PagedRows } from "@/components/app/paged-rows";
import { redirect } from "next/navigation";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getProtocolVersion, listLocations, listProtocolConfigs, type LocationSummary } from "@/lib/api/server";
import { listAllFeedConfigPens } from "@/lib/api/herd-locations";
import { getProcurementLoad } from "@/lib/api/procurement-server";
import type {
  ProcurementArrivalReview,
  ProcurementDecision,
  ProcurementHoldingStay,
  ProcurementLoadDetail,
  ProcurementLoadGoat,
  ProcurementPCHandoff,
  ProcurementSourceHealthCheck,
  ProcurementTimelineEvent,
  ProcurementTransitHandoff,
} from "@/lib/api/procurement";
import { fmtDate, fmtDateTime, shortId } from "@/lib/format";
import { hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { actionFeedbackCopy, copy, optionLabel, optionTitle, optionTone, optionalOption, readableOptionKey, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Tag, TONE_COLOR } from "@/components/ui-primitives";
import type { Tone } from "@/components/ui-primitives";
import { LoadWriteActions } from "./load-forms";
import type { HfRuleOption } from "./hf-rule-picker";
import type { ProcurementLocationOption, ProcurementLocations } from "./location-selects";
import {
  isAcceptedIntake,
  isProcurementHistoryOnly,
  warmupMeta,
} from "./work-state";
import Alert from "@mui/material/Alert";
import type { SxProps, Theme } from "@mui/material/styles";

// Load write surface: a block heading + accordion cards on the page ground. Every field in the
// accordions floats its label over an outlined control — the template TextField (outlined) anatomy,
// applied to the server-rendered form fields so they read as one family with the selects.
const LW_ACTIONS_SX: SxProps<Theme> = { display: "grid", gridTemplateColumns: "minmax(0, 1fr)", gap: 1.5, p: 1 };


function goatLabel(goat: ProcurementLoadGoat): string {
	return goat.animal_identifier_1 || goat.animal_identifier_2 || shortId(goat.goat_id);
}

function contractTone(pageContract: AdminUiPageContract, groupId: string, key: string): Tone {
  return optionTone(pageContract, groupId, key) as Tone;
}

function ContractTag({ pageContract, groupId, value }: { pageContract: AdminUiPageContract; groupId: string; value: string }) {
  return <Tag tone={contractTone(pageContract, groupId, value)}>{optionLabel(pageContract, groupId, value)}</Tag>;
}

function toLocationOption(location: LocationSummary): ProcurementLocationOption {
  return {
    id: location.location_id,
    code: location.location_code,
    name: location.name,
    parentId: location.parent_location_id,
  };
}

function addLocationOption(options: ProcurementLocationOption[], option: ProcurementLocationOption | null): ProcurementLocationOption[] {
  if (!option || options.some((item) => item.id === option.id)) return options;
  return [option, ...options];
}

function sourceLocationOption(load: ProcurementLoadDetail["load"]): ProcurementLocationOption | null {
  if (!load.source_location_id) return null;
  const name = load.source_location_name || load.source_location_code || shortId(load.source_location_id);
  return {
    id: load.source_location_id,
    code: load.source_location_code ?? null,
    name,
    parentId: null,
  };
}

/** Farms and parks a load can come from: the New load drawer's source location picker. */
export async function getProcurementOrigins(): Promise<ProcurementLocationOption[]> {
  // request-plan:ignore owner=procurement-platform issue=C35-016 expires=2026-09-30 reason=fixed two-call location taxonomy request; cardinality does not depend on returned rows
  const [farmsResult, parksResult] = await Promise.all([
    listLocations({ type: "farm", status: "active" }),
    listLocations({ type: "park", status: "active" }),
  ]);
  return [
    ...(farmsResult.ok ? listOrEmpty(farmsResult.data.items).map(toLocationOption) : []),
    ...(parksResult.ok ? listOrEmpty(parksResult.data.items).map(toLocationOption) : []),
  ];
}

/**
 * The published vaccination schedule's doses, for the holding-farm evidence form's dose picker
 * (the evidence endpoint keys on protocol version + rule; nobody should type those ids).
 */
async function getHfRuleOptions(): Promise<HfRuleOption[]> {
  const configs = await listProtocolConfigs("vaccination");
  if (!configs.ok) return [];
  const published = (configs.data.items ?? []).filter((item) => item.status === "published" && item.protocol_version_id);
  // request-plan:ignore owner=procurement-platform issue=C35-016 expires=2026-09-30 reason=one read per published vaccination protocol (a handful), not per row
  const versions = await Promise.all(published.map((item) => getProtocolVersion(item.protocol_version_id)));
  return published.flatMap((item, index) => {
    const version = versions[index];
    if (!version?.ok) return [];
    return [...(version.data.rules ?? [])]
      .sort((a, b) => (a.sort_order ?? a.sequence) - (b.sort_order ?? b.sequence))
      .map((rule) => ({
        protocolVersionId: item.protocol_version_id,
        ruleId: rule.rule_id,
        doseCode: rule.dose_code,
        label: `${item.name} · ${rule.dose_code}`,
      }));
  });
}

function shedUsable(location: LocationSummary): boolean {
  return location.operational.usable_for_vaccination && !location.operational.is_holding;
}

async function getProcurementLocations(): Promise<ProcurementLocations> {
  // request-plan:ignore owner=procurement-platform issue=C35-016 expires=2026-09-30 reason=fixed three-call location taxonomy request; cardinality does not depend on returned rows
  const [parksResult, farmsResult, shedsResult, pensResult] = await Promise.all([
    listLocations({ type: "park", status: "active" }),
    listLocations({ type: "farm", status: "active" }),
    listLocations({ type: "shed", status: "active" }),
    listAllFeedConfigPens(),
  ]);
  const parks = parksResult.ok ? listOrEmpty(parksResult.data.items).map(toLocationOption) : [];
  const usableSheds = shedsResult.ok ? listOrEmpty(shedsResult.data.items).filter(shedUsable).map(toLocationOption) : [];
  const usableShedIds = new Set(usableSheds.map((shed) => shed.id));
  const partitionedShedIds = new Set(
    pensResult.ok ? listOrEmpty(pensResult.data.items).filter((pen) => pen.partition_label).map((pen) => pen.shed_id) : [],
  );
  const penSheds = pensResult.ok
    ? pensResult.data.items
        .filter((pen) => usableShedIds.has(pen.shed_id))
        .map((pen) => ({
          id: pen.shed_id,
          code: null,
          name: pen.shed_name,
          parentId: pen.park_id ?? null,
          partitionLabel: pen.partition_label ?? null,
          operationalLocationDisplay: pen.operational_location_display,
        }))
    : [];
  const sheds = [
    ...usableSheds.filter((shed) => !partitionedShedIds.has(shed.id)),
    ...penSheds,
  ];
  const farms = farmsResult.ok ? listOrEmpty(farmsResult.data.items).map(toLocationOption) : [];
  return {
    parks,
    origins: [...farms, ...parks],
    sheds,
    available: parksResult.ok && shedsResult.ok,
  };
}

function warmupExpectationKey(purpose: string | null | undefined): string {
  if (purpose === "fattening" || purpose === "non_breeding" || purpose === "breeding") return purpose;
  return "unspecified";
}

function WarmupTag({ days, purpose, pageContract }: { days: number | null | undefined; purpose?: string | null; pageContract: AdminUiPageContract }) {
  const warm = warmupMeta(days, purpose);
  const expectationKey = warmupExpectationKey(purpose);
  const dynamicLabel = warm.label === "—" ? copy(pageContract, "label.placeholder") : warm.label;
  return (
    <span title={optionTitle(pageContract, "warmup_expectations", expectationKey)}>
      <Tag tone={warm.tone}>
        {dynamicLabel} / {optionLabel(pageContract, "warmup_expectations", expectationKey)}
      </Tag>
    </span>
  );
}

// ---- Template detail blocks ----
// Every secondary record set is the template order-details card: Card + CardHeader (title, a soft
// Label count as the header action, an optional subheader note) with the table scrolling inside the
// card's Scrollbar (the template's order-details-items Scrollbar box), never past the page edge.
function DetailTableCard({
  title,
  count,
  tone = "info",
  note,
  children,
}: {
  title: string;
  count?: number;
  tone?: Tone;
  note?: string;
  children: React.ReactNode;
}) {
  return (
    <Card component="section" aria-label={title}>
      <CardHeader
        title={title}
        subheader={note}
        slotProps={{ title: { component: "h3" } }}
        action={count === undefined ? undefined : <Label variant="soft" color={count ? TONE_COLOR[tone] : "default"}>{count}</Label>}
        sx={{ mb: 2 }}
      />
      {children}
    </Card>
  );
}

function DetailTable({ labels, ariaLabel, children }: { labels: string[]; ariaLabel: string; children: React.ReactNode }) {
  return (
    <Scrollbar>
      <Table sx={{ minWidth: 640 }} aria-label={ariaLabel}>
        <TableHeadCustom headCells={labels.map((label, index) => ({ id: `c${index}`, label, sortable: false }))} />
        <TableBody>{children}</TableBody>
      </Table>
    </Scrollbar>
  );
}

/** Contract labels written as mid-sentence words ("purchase") read as field labels in the rail. */
function cap(label: string): string {
  return label ? label.charAt(0).toUpperCase() + label.slice(1) : label;
}

const GOAT_ID_SX = { typography: "subtitle2", whiteSpace: "nowrap" } as const;
const MUTED_SX = { color: "text.secondary" } as const;

// ---- Journey timeline ----
// A goat event carries a goat state, a load event a load status; each has its own contract group.
// The chip shows the farm label ("Accepted intake"), never the stored key (`accepted_herd_intake`).
function timelineStateLabel(pageContract: AdminUiPageContract, event: ProcurementTimelineEvent): string {
  const state = event.state ?? "";
  const groups = event.goat_id ? ["proc_goat_state", "proc_selection_state"] : ["source_load_status", "proc_goat_state"];
  for (const groupId of groups) {
    const option = optionalOption(pageContract, groupId, state);
    if (option) return option.label;
  }
  return readableOptionKey(state);
}

function timelineItems(events: ProcurementTimelineEvent[], goats: ProcurementLoadGoat[], pageContract: AdminUiPageContract): OrderHistoryItem[] {
  const goatById = new Map(goats.map((goat) => [goat.goat_id, goat]));
  return events.map((event, idx) => {
    const goat = event.goat_id
      ? `${copy(pageContract, "label.goat_prefix")} ${goatById.has(event.goat_id) ? goatLabel(goatById.get(event.goat_id) as ProcurementLoadGoat) : shortId(event.goat_id)}`
      : null;
    return {
      key: `${event.ref_id ?? event.event_type}-${idx}`,
      title: (
        <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
          {event.summary || event.event_type || copy(pageContract, "label.event_fallback")}
          {event.state ? <Tag tone="info">{timelineStateLabel(pageContract, event)}</Tag> : null}
        </Box>
      ),
      body: goat ?? undefined,
      time: event.occurred_at ? fmtDateTime(event.occurred_at) : undefined,
    };
  });
}

// ---- Per-goat rows ----
function GoatRows({ goats, pageContract }: { goats: ProcurementLoadGoat[]; pageContract: AdminUiPageContract }) {
  const goatCols = tableLabels(pageContract, "load-goats");
  return (
    <DetailTableCard title={copy(pageContract, "section.goats.title")} count={goats.length}>
      <PagedRows
        /* main's mobile-scroll fix (56b3da919) is these two class names: `.twrap` owns the
           horizontal scroll and `.procurement-load-goats-table` carries the per-column
           min-widths. PagedRows already supplies the tabIndex/role/aria-label that fix added. */
        wrapClassName="twrap"
        tableClassName="procurement-load-goats-table"
        ariaLabel={copy(pageContract, "section.goats.title")}
        head={
          <TableRow>
            {goatCols.map((c) => (
              <TableCell component="th" key={c}>{c}</TableCell>
            ))}
          </TableRow>
        }
        empty={
          <TableRow>
            <TableCell colSpan={goatCols.length} sx={{ py: 3, textAlign: "center", ...MUTED_SX, typography: "body2" }}>
              {copy(pageContract, "empty.load_goats")}
            </TableCell>
          </TableRow>
        }
        rows={goats.map((goat) => {
                const accepted = isAcceptedIntake(goat.current_state);
                const historyOnly = isProcurementHistoryOnly(goat.current_state);
                return (
                  <TableRow key={goat.load_goat_id} hover>
                    <TableCell sx={GOAT_ID_SX}>{goatLabel(goat)}</TableCell>
                    <TableCell>
                      <ContractTag pageContract={pageContract} groupId="proc_selection_state" value={goat.selection_state} />
                    </TableCell>
                    <TableCell>
                      <ContractTag pageContract={pageContract} groupId="proc_goat_state" value={goat.current_state} />
                    </TableCell>
                    <TableCell>
                      <ContractTag pageContract={pageContract} groupId="proc_source_entry_state" value={goat.source_entry_state} />
                    </TableCell>
                    <TableCell>
                      <ContractTag pageContract={pageContract} groupId="proc_ownership_state" value={goat.ownership_state} />
                    </TableCell>
                    <TableCell>
                      <ContractTag pageContract={pageContract} groupId="proc_health_state" value={goat.health_state} />
                    </TableCell>
                    <TableCell>
                      <WarmupTag days={goat.warmup_days} purpose={goat.purpose} pageContract={pageContract} />
                    </TableCell>
                    <TableCell sx={{ typography: "body2" }}>
                      {accepted && goat.goat_id ? (
                        <MuiLink component={Link} href={`/goats/${encodeURIComponent(goat.goat_id)}`} color="inherit" underline="always">
                          {copy(pageContract, "action.pc_passport")}
                        </MuiLink>
                      ) : historyOnly ? (
                        <Box component="span" sx={MUTED_SX}>{copy(pageContract, "label.procurement_history_no_pc")}</Box>
                      ) : (
                        <Box component="span" sx={MUTED_SX}>{copy(pageContract, "label.in_source_entry")}</Box>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
      />
    </DetailTableCard>
  );
}

function placeholderOr(pageContract: AdminUiPageContract, value: React.ReactNode): React.ReactNode {
  return value || <Box component="span" sx={MUTED_SX}>{copy(pageContract, "label.placeholder")}</Box>;
}

// ---- Pre-dispatch decisions ----
function DecisionCard({ decisions, pageContract }: { decisions: ProcurementDecision[]; pageContract: AdminUiPageContract }) {
  const labels = tableLabels(pageContract, "pre-dispatch-decisions");
  return (
    <DetailTableCard title={copy(pageContract, "section.pre_dispatch.title")} count={decisions.length} tone="warn">
      <DetailTable labels={labels} ariaLabel={copy(pageContract, "section.pre_dispatch.title")}>
        {decisions.map((d, idx) => (
          <TableRow key={d.decision_id ?? idx}>
            <TableCell sx={GOAT_ID_SX}>{shortId(d.goat_id)}</TableCell>
            <TableCell sx={MUTED_SX}>{d.decision_stage ?? "pre_dispatch"}</TableCell>
            <TableCell>{placeholderOr(pageContract, d.decision_type ? <ContractTag pageContract={pageContract} groupId="proc_decision_type" value={d.decision_type} /> : null)}</TableCell>
            <TableCell sx={MUTED_SX}>{d.reason ?? copy(pageContract, "label.placeholder")}</TableCell>
            <TableCell sx={MUTED_SX}>{fmtDateTime(d.decided_at) || copy(pageContract, "label.placeholder")}</TableCell>
          </TableRow>
        ))}
      </DetailTable>
    </DetailTableCard>
  );
}

// ---- Arrival gate (distinct checkpoint) ----
function ArrivalGateCard({ reviews, pageContract }: { reviews: ProcurementArrivalReview[]; pageContract: AdminUiPageContract }) {
  const labels = tableLabels(pageContract, "arrival-goats");
  return (
    <DetailTableCard title={copy(pageContract, "section.arrival_gate.title")} count={reviews.length} tone="pur">
      <Stack spacing={3} sx={{ pb: 1 }}>
        {reviews.map((review, idx) => {
          const goats = review.goats ?? [];
          const matched = goats.filter((g) => g.arrival_state === "matched" || g.arrival_state === "accepted").length;
          const missing = goats.filter((g) => g.arrival_state === "missing").length;
          const extra = goats.filter((g) => g.arrival_state === "extra_unresolved").length;
          return (
            <Box key={review.review_id ?? idx}>
              <Box sx={{ px: 3, pb: 2, display: "flex", gap: 1, flexWrap: "wrap", alignItems: "center", typography: "body2" }}>
                {review.status ? <Tag tone={contractTone(pageContract, "proc_arrival_status", review.status)}>{copy(pageContract, "label.arrival_prefix")}: {optionLabel(pageContract, "proc_arrival_status", review.status)}</Tag> : null}
                <Box component="span" sx={MUTED_SX}>{copy(pageContract, "label.park_prefix")} {review.park_location_label || (review.park_location_id ? shortId(review.park_location_id) : copy(pageContract, "label.placeholder"))}</Box>
                <Box sx={{ flexGrow: 1 }} />
                <Box component="span" sx={MUTED_SX}>
                  {matched} {copy(pageContract, "label.matched")} · {missing} {copy(pageContract, "label.missing")} · {extra} {copy(pageContract, "label.extra_unknown")} · {goats.length} {copy(pageContract, "label.reviewed")}
                </Box>
              </Box>
              {goats.length > 0 ? (
                <DetailTable labels={labels} ariaLabel={table(pageContract, "arrival-goats").title}>
                  {goats.map((g, gi) => (
                    <TableRow key={g.review_goat_id ?? gi}>
                      <TableCell sx={GOAT_ID_SX}>{shortId(g.goat_id)}</TableCell>
                      <TableCell>{placeholderOr(pageContract, g.arrival_state ? <ContractTag pageContract={pageContract} groupId="proc_arrival_state" value={g.arrival_state} /> : null)}</TableCell>
                    </TableRow>
                  ))}
                </DetailTable>
              ) : null}
            </Box>
          );
        })}
      </Stack>
    </DetailTableCard>
  );
}

// ---- Supporting records (transit, holding, source health, Preventive Care (PC) handoff) ----
function TransitCard({ handoffs, pageContract }: { handoffs: ProcurementTransitHandoff[]; pageContract: AdminUiPageContract }) {
  if (handoffs.length === 0) return null;
  const labels = tableLabels(pageContract, "transit-handoffs");
  return (
    <DetailTableCard title={copy(pageContract, "section.transit.title")} count={handoffs.length}>
      <DetailTable labels={labels} ariaLabel={copy(pageContract, "section.transit.title")}>
        {handoffs.map((h, idx) => (
          <TableRow key={h.handoff_id ?? idx}>
            <TableCell>{h.loaded_count ?? copy(pageContract, "label.placeholder")}</TableCell>
            <TableCell sx={MUTED_SX}>{h.from_location_label || (h.from_location_id ? shortId(h.from_location_id) : copy(pageContract, "label.placeholder"))}</TableCell>
            <TableCell sx={MUTED_SX}>{h.to_location_label || (h.to_location_id ? shortId(h.to_location_id) : copy(pageContract, "label.placeholder"))}</TableCell>
            <TableCell sx={MUTED_SX}>{fmtDateTime(h.dispatched_at) || copy(pageContract, "label.placeholder")}</TableCell>
            <TableCell>{placeholderOr(pageContract, h.status ? <ContractTag pageContract={pageContract} groupId="proc_transit_status" value={h.status} /> : null)}</TableCell>
            <TableCell>{placeholderOr(pageContract, h.discrepancy_state ? <ContractTag pageContract={pageContract} groupId="proc_discrepancy_state" value={h.discrepancy_state} /> : null)}</TableCell>
          </TableRow>
        ))}
      </DetailTable>
    </DetailTableCard>
  );
}

function HoldingCard({ stays, pageContract }: { stays: ProcurementHoldingStay[]; pageContract: AdminUiPageContract }) {
  if (stays.length === 0) return null;
  const labels = tableLabels(pageContract, "holding-stays");
  return (
    <DetailTableCard title={copy(pageContract, "section.holding.title")} count={stays.length} note={copy(pageContract, "section.holding.note")}>
      <DetailTable labels={labels} ariaLabel={copy(pageContract, "section.holding.title")}>
        {stays.map((s, idx) => (
          <TableRow key={s.stay_id ?? idx}>
            <TableCell sx={GOAT_ID_SX}>{shortId(s.goat_id)}</TableCell>
            <TableCell sx={MUTED_SX}>{s.holding_location_id ? shortId(s.holding_location_id) : copy(pageContract, "label.placeholder")}</TableCell>
            <TableCell sx={MUTED_SX}>{fmtDate(s.started_at) || copy(pageContract, "label.placeholder")}</TableCell>
            <TableCell sx={MUTED_SX}>{s.ended_at ? fmtDate(s.ended_at) : copy(pageContract, "label.ongoing")}</TableCell>
            <TableCell>
              <WarmupTag days={s.warmup_days} purpose={s.purpose} pageContract={pageContract} />
            </TableCell>
            <TableCell>{placeholderOr(pageContract, s.warmup_state ? <ContractTag pageContract={pageContract} groupId="proc_warmup_state" value={s.warmup_state} /> : null)}</TableCell>
          </TableRow>
        ))}
      </DetailTable>
    </DetailTableCard>
  );
}

function HealthCard({ checks, pageContract }: { checks: ProcurementSourceHealthCheck[]; pageContract: AdminUiPageContract }) {
  if (checks.length === 0) return null;
  const labels = tableLabels(pageContract, "source-health-checks");
  return (
    <DetailTableCard title={copy(pageContract, "section.source_health.title")} count={checks.length}>
      <DetailTable labels={labels} ariaLabel={copy(pageContract, "section.source_health.title")}>
        {checks.map((c, idx) => (
          <TableRow key={c.health_check_id ?? idx}>
            <TableCell sx={GOAT_ID_SX}>{shortId(c.goat_id)}</TableCell>
            <TableCell>{placeholderOr(pageContract, c.health_state ? <ContractTag pageContract={pageContract} groupId="proc_health_state" value={c.health_state} /> : null)}</TableCell>
            <TableCell sx={MUTED_SX}>{fmtDateTime(c.checked_at) || copy(pageContract, "label.placeholder")}</TableCell>
          </TableRow>
        ))}
      </DetailTable>
    </DetailTableCard>
  );
}

function HandoffCard({ handoffs, pageContract }: { handoffs: ProcurementPCHandoff[]; pageContract: AdminUiPageContract }) {
  if (handoffs.length === 0) return null;
  const labels = tableLabels(pageContract, "pc-handoffs");
  return (
    <DetailTableCard title={copy(pageContract, "section.pc_handoffs.title")} count={handoffs.length} tone="ok" note={copy(pageContract, "section.pc_handoffs.note")}>
      <DetailTable labels={labels} ariaLabel={copy(pageContract, "section.pc_handoffs.title")}>
        {handoffs.map((h, idx) => (
          <TableRow key={h.handoff_id ?? idx}>
            <TableCell sx={GOAT_ID_SX}>
              {h.goat_id ? (
                <MuiLink component={Link} href={`/goats/${encodeURIComponent(h.goat_id)}`} color="inherit" underline="always">
                  {shortId(h.goat_id)}
                </MuiLink>
              ) : (
                copy(pageContract, "label.placeholder")
              )}
            </TableCell>
            <TableCell sx={MUTED_SX}>{h.park_location_label || (h.park_location_id ? shortId(h.park_location_id) : copy(pageContract, "label.placeholder"))}</TableCell>
            <TableCell sx={MUTED_SX}>{h.shed_location_label || (h.shed_location_id ? shortId(h.shed_location_id) : copy(pageContract, "label.placeholder"))}</TableCell>
            <TableCell sx={MUTED_SX}>{fmtDate(h.entry_date) || copy(pageContract, "label.placeholder")}</TableCell>
            <TableCell sx={MUTED_SX}>{fmtDateTime(h.accepted_at) || copy(pageContract, "label.placeholder")}</TableCell>
            <TableCell>{placeholderOr(pageContract, h.event_status ? <ContractTag pageContract={pageContract} groupId="proc_handoff_status" value={h.event_status} /> : null)}</TableCell>
          </TableRow>
        ))}
      </DetailTable>
    </DetailTableCard>
  );
}

export async function ProcurementLoadDetailPage({
  loadId,
  searchParams,
  pageContract,
}: {
  loadId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const [result, locations, hfRuleOptions] = await Promise.all([getProcurementLoad(loadId), getProcurementLocations(), getHfRuleOptions()]);
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const sp = searchParams ?? {};
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const returnTo = hrefWithoutAction(`/procurement/source-entry/loads/${encodeURIComponent(loadId)}`, sp);
  const backHref = hrefWithoutAction("/procurement/source-entry", sp);
  const backLabel = copy(pageContract, "action.back_source_entry");

  if (!result.ok) {
    return (
      <div className="screen on">
        <OrderDetailsToolbar title={pageContract.title || copy(pageContract, "fallback.title")} backHref={backHref} backLabel={backLabel} />
        <Alert severity="error" sx={{ mb: 3 }}>
          {result.error.message}
        </Alert>
        <Button component={Link} href={backHref} color="inherit" variant="outlined" startIcon={<Iconify icon="eva:arrow-ios-back-fill" />}>
          {backLabel}
        </Button>
      </div>
    );
  }

  const detail: ProcurementLoadDetail = result.data.detail;
  const { load } = detail;
  const goats = detail.goats ?? [];
  const hfEvidence = detail.hf_vaccination_evidence ?? [];
  const decisions = detail.decisions ?? [];
  const arrivalReviews = detail.arrival_reviews ?? [];
  const transitHandoffs = detail.transit_handoffs ?? [];
  const holdingStays = detail.holding_stays ?? [];
  const sourceHealthChecks = detail.source_health_checks ?? [];
  const pcHandoffs = detail.pc_handoffs ?? [];
  const timeline = detail.timeline ?? [];
  const sourceParty = load.source_party_name || shortId(load.source_party_id);
  const sourceLocation = load.source_location_name || load.source_location_code || null;
  const title = `${sourceLocation ?? copy(pageContract, "label.holding_farm")} · ${sourceParty}`;
  const loadLocations = {
    ...locations,
    origins: addLocationOption(locations.origins, sourceLocationOption(load)),
  };
  const placeholder = copy(pageContract, "label.placeholder");
  const purchase = fmtDate(load.purchase_date ?? undefined) || placeholder;
  const plannedDispatch = fmtDate(load.planned_dispatch_at ?? undefined) || placeholder;

  return (
    <div className="screen on">
      {/* Template order details (sections/order/view/order-details-view): toolbar with back arrow,
          heading + status Label and the date line; Grid md 8 / 4 with the record cards and the
          History timeline on the left and the Customer / Delivery rail on the right. */}
      <OrderDetailsToolbar
        title={title}
        status={optionLabel(pageContract, "source_load_status", load.status)}
        statusColor={TONE_COLOR[contractTone(pageContract, "source_load_status", load.status)]}
        backHref={backHref}
        backLabel={backLabel}
        subtitle={[sourceParty, fmtDate(load.purchase_date ?? undefined)].filter(Boolean).join(" · ")}
      />

      {actionStatus ? (
        <Alert severity={actionStatus === "success" ? "success" : "error"} sx={{ mb: 3 }}>
          {actionStatus === "success" ? null : <b>{copy(pageContract, "action.failed_title")}&nbsp;</b>}
          {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
        </Alert>
      ) : null}

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 8 }}>
          <Stack spacing={3}>
            <GoatRows goats={goats} pageContract={pageContract} />

            {/* Operator write surface — every control submits a real server action (idempotency-keyed). */}
            <Card component="section" className="lw-actions" aria-label={copy(pageContract, "section.actions.title")}>
              <CardHeader title={copy(pageContract, "section.actions.title")} slotProps={{ title: { component: "h3" } }} />
              <Box sx={LW_ACTIONS_SX}>
              <LoadWriteActions
                loadId={load.load_id}
                goats={goats}
                hfEvidence={hfEvidence}
                hfRuleOptions={hfRuleOptions}
                returnTo={returnTo}
                pageContract={pageContract}
                locations={loadLocations}
                defaultFromLocationId={load.source_location_id ?? ""}
              />
              </Box>
            </Card>

            {/* Empty 0-count cards are not rendered: a card that only says "nothing yet" is a slot
                without content; the journey timeline below still shows every step. */}
            {decisions.length > 0 ? <DecisionCard decisions={decisions} pageContract={pageContract} /> : null}
            {arrivalReviews.length > 0 ? <ArrivalGateCard reviews={arrivalReviews} pageContract={pageContract} /> : null}
            <TransitCard handoffs={transitHandoffs} pageContract={pageContract} />
            <HoldingCard stays={holdingStays} pageContract={pageContract} />
            <HealthCard checks={sourceHealthChecks} pageContract={pageContract} />
            <HandoffCard handoffs={pcHandoffs} pageContract={pageContract} />
            <OrderDetailsHistory
              title={
                <>
                  {copy(pageContract, "section.timeline.title")}
                  {/* CardHeader subheader slot: the stage order note, under the title at every width. */}
                  <Typography component="span" variant="body2" sx={{ display: "block", mt: 0.5, color: "text.secondary" }}>
                    {copy(pageContract, "section.timeline.note")}
                  </Typography>
                </>
              }
              timeline={timelineItems(timeline, goats, pageContract)}
              summary={
                timeline.length === 0
                  ? [{ key: "empty", label: copy(pageContract, "empty.timeline"), value: null }]
                  : [
                      { key: "purchase", label: cap(copy(pageContract, "label.purchase")), value: purchase },
                      { key: "dispatch", label: cap(copy(pageContract, "label.planned_dispatch")), value: plannedDispatch },
                    ]
              }
            />
          </Stack>
        </Grid>

        <Grid size={{ xs: 12, md: 4 }}>
          <Card>
            <OrderDetailsCustomer title="Source party" name={sourceParty} lines={[sourceLocation ?? placeholder]} />

            <Divider sx={{ borderStyle: "dashed" }} />
            <OrderDetailsDelivery
              title={copy(pageContract, "label.load")}
              rows={[
                { key: "holding", label: cap(copy(pageContract, "label.holding_farm")), value: sourceLocation ?? placeholder },
                { key: "expected", label: cap(copy(pageContract, "label.expected")), value: load.expected_count },
                { key: "purchase", label: cap(copy(pageContract, "label.purchase")), value: purchase },
                { key: "dispatch", label: cap(copy(pageContract, "label.planned_dispatch")), value: plannedDispatch },
              ]}
            />
          </Card>
        </Grid>
      </Grid>
    </div>
  );
}
