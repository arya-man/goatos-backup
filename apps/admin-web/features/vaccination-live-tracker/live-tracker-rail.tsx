import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Chip from "@mui/material/Chip";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { Label } from "@/components/minimal/label";
import Link from "@/components/no-prefetch-link";
import { copy, optionalOption, optionLabel, optionTone, optionTitle, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  LiveTrackerActivity,
  LiveTrackerActivityItem,
  LiveTrackerAttentionRow,
  LiveTrackerVerification,
} from "@/lib/api/vaccination-live-tracker";
import { fmtClock, paletteOf } from "./format";
import { LiveDot, LiveEmpty } from "./live-ui";

type ActivityAnimalRow = {
  key: string;
  newestAt: string;
  actorName: string;
  shedLabel: string;
  vaccineLabel: string;
  scannedIdentifier: string;
  detailCode: string;
  kinds: Set<string>;
};

function animalKey(item: LiveTrackerActivityItem): string {
  return item.goat_id || item.scanned_identifier || item.event_id;
}

function activityAnimalRows(items: LiveTrackerActivityItem[]): ActivityAnimalRow[] {
  const rows = new Map<string, ActivityAnimalRow>();
  for (const item of items) {
    const key = animalKey(item);
    const existing = rows.get(key);
    if (existing) {
      existing.kinds.add(item.kind);
      if (item.occurred_at > existing.newestAt) {
        existing.newestAt = item.occurred_at;
        existing.actorName = item.actor_name || existing.actorName;
        existing.shedLabel = item.shed_label || existing.shedLabel;
        existing.vaccineLabel = item.vaccine_label || existing.vaccineLabel;
        existing.scannedIdentifier = item.scanned_identifier || existing.scannedIdentifier;
        existing.detailCode = item.detail_code || existing.detailCode;
      } else {
        existing.actorName ||= item.actor_name;
        existing.shedLabel ||= item.shed_label;
        existing.vaccineLabel ||= item.vaccine_label;
        existing.scannedIdentifier ||= item.scanned_identifier;
        existing.detailCode ||= item.detail_code;
      }
      continue;
    }
    rows.set(key, {
      key,
      newestAt: item.occurred_at,
      actorName: item.actor_name,
      shedLabel: item.shed_label,
      vaccineLabel: item.vaccine_label,
      scannedIdentifier: item.scanned_identifier,
      detailCode: item.detail_code,
      kinds: new Set([item.kind]),
    });
  }
  return [...rows.values()].sort((a, b) => b.newestAt.localeCompare(a.newestAt));
}

function uniqueScanRate(items: LiveTrackerActivityItem[]): number | null {
  const scans = items.filter((item) => item.kind === "scan_capture" && item.goat_id);
  if (scans.length < 2) return null;
  const newest = new Date(scans.reduce((max, item) => (item.occurred_at > max ? item.occurred_at : max), scans[0].occurred_at));
  const oldest = new Date(scans.reduce((min, item) => (item.occurred_at < min ? item.occurred_at : min), scans[0].occurred_at));
  const minutes = (newest.getTime() - oldest.getTime()) / 60000;
  if (!(minutes > 0)) return null;
  const unique = new Set(scans.map((item) => item.goat_id)).size;
  return Math.round((unique / minutes) * 10) / 10;
}

function animalActivityLabel(row: ActivityAnimalRow, pageContract: AdminUiPageContract): string {
  const labels = [];
  if (row.kinds.has("scan_capture")) labels.push(optionLabel(pageContract, "live_activity_kind", "scan_capture"));
  if (row.kinds.has("proof_video")) labels.push(optionLabel(pageContract, "live_activity_kind", "proof_video"));
  if (row.kinds.has("administration")) labels.push(optionLabel(pageContract, "live_activity_kind", "administration"));
  if (row.kinds.has("obligation_closed")) labels.push(optionLabel(pageContract, "live_activity_kind", "obligation_closed"));
  if (row.kinds.has("scan_duplicate")) labels.push(optionLabel(pageContract, "live_activity_kind", "scan_duplicate"));
  if (row.kinds.has("scan_unknown")) labels.push(optionLabel(pageContract, "live_activity_kind", "scan_unknown"));
  return labels.join(" + ");
}

// Live activity. Built from the canonical event tables (proof uploads, scan captures, scan attempts,
// completions, obligation status events) — NOT from the audit log, which carries no scan and no
// proof event and would have produced a feed that silently omitted the two things this page exists
// to show.
function ActivityCard({
  activity,
  scanCaptureTotal,
  proofVideoTotal,
  pageContract,
}: {
  activity: LiveTrackerActivity;
  scanCaptureTotal: number;
  proofVideoTotal: number;
  pageContract: AdminUiPageContract;
}) {
  const placeholder = copy(pageContract, "label.placeholder");
  const animalRows = activityAnimalRows(activity.items);
  const scanRate = uniqueScanRate(activity.items);
  return (
    <Card id="lt-activity">
      <CardHeader
        title={copy(pageContract, "section.activity.title")}
        action={
          <Label variant="soft" color="error" startIcon={<LiveDot color="error" size={6} />}>
            {scanRate == null ? copy(pageContract, "live.feed_rate_unavailable") : `${scanRate} ${copy(pageContract, "live.scan_rate_suffix")}`}
          </Label>
        }
        slotProps={{ action: { sx: { alignSelf: "center" } } }}
      />
      {activity.items.length > 0 ? (
        // Template summary strip (booking / invoice analytic): figure over caption, dashed dividers.
        <Box sx={{ mx: 3, mt: 2, display: "grid", gridTemplateColumns: "repeat(3, 1fr)", borderRadius: "var(--r-lg)", border: 1, borderColor: "divider", borderStyle: "dashed" }}>
          {[
            [scanCaptureTotal, copy(pageContract, "live.scanned_label")],
            [proofVideoTotal, copy(pageContract, "live.proofed_label")],
            [activity.items.length, copy(pageContract, "live.events_label")],
          ].map(([value, label], index) => (
            <Box key={String(label)} sx={{ py: 1.5, textAlign: "center", borderLeft: index ? 1 : 0, borderColor: "divider", borderLeftStyle: "dashed" }}>
              <Typography variant="subtitle1">{value}</Typography>
              <Typography variant="caption" sx={{ color: "text.secondary" }}>
                {label}
              </Typography>
            </Box>
          ))}
        </Box>
      ) : null}
      {activity.items.length > 0 && activity.observed_per_min != null ? (
        <Typography variant="caption" component="div" sx={{ px: 3, pt: 1, color: "text.secondary" }}>
          {activity.observed_per_min}
          {copy(pageContract, "live.feed_rate_suffix")} {copy(pageContract, "live.events_label")}
        </Typography>
      ) : null}
      <Box aria-live="polite" aria-label={copy(pageContract, "live.feed_aria")} tabIndex={0} role="log" sx={{ maxHeight: 420, overflowY: "auto", overscrollBehavior: "contain" }}>
        {activity.items.length === 0 ? (
          <Box sx={{ pt: 2 }}>
            <LiveEmpty title={copy(pageContract, "section.activity.empty_title")} body={copy(pageContract, "section.activity.empty_body")} />
          </Box>
        ) : (
          // Template list rows (AppTopAuthors / ecommerce latest products): dot, primary + caption, time.
          <Stack spacing={2} sx={{ p: 3 }}>
            {animalRows.map((row) => {
              const label = animalActivityLabel(row, pageContract) || placeholder;
              const detail = row.detailCode ? optionalOption(pageContract, "live_activity_detail", row.detailCode)?.label ?? "" : "";
              const meta = [row.shedLabel, row.vaccineLabel, row.scannedIdentifier, detail].filter(Boolean).join(" · ");
              const tone = row.kinds.has("scan_capture") ? "info" : optionTone(pageContract, "live_activity_kind", [...row.kinds][0]) || "mut";
              return (
                <Box key={row.key} sx={{ display: "flex", alignItems: "flex-start", gap: 1.5, minWidth: 0 }}>
                  <Box sx={{ pt: 0.75 }}>
                    <LiveDot color={paletteOf(tone)} />
                  </Box>
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography variant="subtitle2" noWrap>
                      {row.actorName || placeholder}
                      <Box component="span" sx={{ color: "text.secondary", fontWeight: "fontWeightRegular" }}>
                        {" · "}
                        {label}
                      </Box>
                    </Typography>
                    <Typography variant="caption" component="div" sx={{ color: "text.disabled", overflowWrap: "anywhere" }}>
                      {meta || placeholder}
                    </Typography>
                  </Box>
                  <Typography variant="caption" sx={{ color: "text.disabled", whiteSpace: "nowrap" }}>
                    {fmtClock(row.newestAt)}
                  </Typography>
                </Box>
              );
            })}
          </Stack>
        )}
      </Box>
    </Card>
  );
}

// Attention. Each row is derived from the same rollups the tables render, so the count chip equals
// the number of rows by construction rather than by a constant that never updated.
//
// Three of the mock's attention affordances have no record behind them at all — a nudge dispatch, an
// escalation deadline, and a "finishes at current pace" projection all require fields that are not
// modelled. They stay in place, mock-shaped, disabled and carrying the reason.
function AttentionCard({
  attention,
  total,
  truncated,
  pageContract,
}: {
  attention: LiveTrackerAttentionRow[];
  total: number;
  truncated: boolean;
  pageContract: AdminUiPageContract;
}) {
  return (
    <Card id="lt-attention">
      <CardHeader
        title={copy(pageContract, "section.attention.title")}
        action={
          <Label variant="soft" color="warning">
            {total}
          </Label>
        }
        slotProps={{ action: { sx: { alignSelf: "center" } } }}
      />
      <Stack spacing={2} sx={{ p: 3 }}>
        {truncated ? (
          <Typography variant="caption" data-truncnote="" role="status" sx={{ color: "text.secondary" }}>
            <b>
              {attention.length}/{total}
            </b>{" "}
            {copy(pageContract, "section.attention.truncated_note")}
          </Typography>
        ) : null}
        {attention.length === 0 ? (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "section.attention.empty")}
          </Typography>
        ) : (
          attention.map((row, index) => (
            <Box key={`${row.kind}|${row.shed_id}|${row.operator_id}|${index}`} sx={{ p: 2, borderRadius: "var(--r-lg)", bgcolor: "background.neutral" }}>
              <Typography variant="subtitle2">
                {row.subject_label}
                <Box component="span" sx={{ color: "warning.main" }}>
                  {" · "}
                  {optionLabel(pageContract, "live_attention_kind", row.kind)}
                </Box>
              </Typography>
              <Typography variant="caption" component="div" sx={{ mt: 0.5, color: "text.secondary" }}>
                {row.metric_count}
                {row.total_count > 0 ? `/${row.total_count}` : null}
                {/* Never a bare integer: the elapsed figure always carries its unit. */}
                {row.elapsed_minutes > 0 ? ` · ${row.elapsed_minutes} ${copy(pageContract, "section.attention.elapsed_suffix")}` : null}
                {row.since_at ? ` · ${fmtClock(row.since_at)}` : null}
                {" · "}
                {optionTitle(pageContract, "live_attention_kind", row.kind)}
              </Typography>
              {/* These three have no record behind them: the labels name the missing capability and
                  the reasons render as visible text below (never a hover title). */}
              <Box sx={{ mt: 1.5, display: "flex", flexWrap: "wrap", gap: 0.75 }}>
                <Chip size="small" variant="outlined" disabled aria-disabled="true" label={copy(pageContract, "section.attention.nudge_label")} />
                <Chip size="small" variant="outlined" disabled aria-disabled="true" label={copy(pageContract, "section.attention.escalate_label")} />
                <Chip size="small" variant="outlined" disabled aria-disabled="true" label={copy(pageContract, "section.attention.pace_label")} />
              </Box>
              <Typography variant="caption" component="div" data-truncnote="" sx={{ mt: 1, color: "text.disabled" }}>
                {copy(pageContract, "section.attention.nudge")} {copy(pageContract, "section.attention.escalation")}{" "}
                {copy(pageContract, "section.attention.pace")}
              </Typography>
            </Box>
          ))
        )}
      </Stack>
    </Card>
  );
}

function VerificationCard({
  verification,
  verifyHref,
  pageContract,
}: {
  verification: LiveTrackerVerification;
  verifyHref: string;
  pageContract: AdminUiPageContract;
}) {
  const shedsSuffix = (count: number) =>
    copy(pageContract, count === 1 ? "section.verification.sheds_suffix_one" : "section.verification.sheds_suffix");
  const figures: Array<[string, string, string]> = [
    [copy(pageContract, "section.verification.awaiting"), `${verification.awaiting_review_sheds} ${shedsSuffix(verification.awaiting_review_sheds)}`, "text.primary"],
    [copy(pageContract, "section.verification.verified"), `${verification.verified_today_sheds} ${shedsSuffix(verification.verified_today_sheds)}`, "success.main"],
    [copy(pageContract, "section.verification.rework"), String(verification.rework_requested), "warning.main"],
  ];
  // Template summary rows (invoice / checkout summary): label left, figure right, action below.
  return (
    <Card id="lt-verification">
      <CardHeader
        title={copy(pageContract, "section.verification.title")}
        action={
          <Typography variant="caption" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "section.verification.badge")}
          </Typography>
        }
        slotProps={{ action: { sx: { alignSelf: "center" } } }}
      />
      <Stack spacing={1.5} sx={{ p: 3 }}>
        {figures.map(([label, value, color]) => (
          <Box key={label} sx={{ display: "flex", justifyContent: "space-between", gap: 2, typography: "body2" }}>
            <Box component="span" sx={{ color: "text.secondary" }}>
              {label}
            </Box>
            <Box component="span" sx={{ typography: "subtitle2", color, whiteSpace: "nowrap" }}>
              {value}
            </Box>
          </Box>
        ))}
        {/* The counts above are park-scoped, and /verify reads parseScope: the hand-off carries the
            same scope or the two screens disagree about the same queue. */}
        <Box sx={{ pt: 1 }}>
          <Button component={Link} href={verifyHref} size="small" variant="outlined" color="inherit">
            {copy(pageContract, "action.open_verify")}
          </Button>
        </Box>
      </Stack>
    </Card>
  );
}

export function LiveTrackerRail({
  activity,
  scanCaptureTotal,
  proofVideoTotal,
  attention,
  attentionTotal,
  attentionTruncated,
  verification,
  verifyHref,
  pageContract,
}: {
  activity: LiveTrackerActivity;
  scanCaptureTotal: number;
  proofVideoTotal: number;
  attention: LiveTrackerAttentionRow[];
  attentionTotal: number;
  attentionTruncated: boolean;
  verification: LiveTrackerVerification;
  verifyHref: string;
  pageContract: AdminUiPageContract;
}) {
  return (
    <Stack spacing={3}>
      <ActivityCard
        activity={activity}
        scanCaptureTotal={scanCaptureTotal}
        proofVideoTotal={proofVideoTotal}
        pageContract={pageContract}
      />
      <AttentionCard
        attention={attention}
        total={attentionTotal}
        truncated={attentionTruncated}
        pageContract={pageContract}
      />
      <VerificationCard verification={verification} verifyHref={verifyHref} pageContract={pageContract} />
    </Stack>
  );
}
