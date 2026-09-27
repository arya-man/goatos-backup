import Alert from "@mui/material/Alert";
import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { DividedStack } from "@/components/app/divided-stack";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { Tag } from "@/components/ui-primitives";
import type { HerdGateway } from "@/lib/api/herd-signals";
import { operationalLocationLabel } from "@/lib/operational-location";
import { fmtAgo, fmtBleMac } from "./format";
import { EmptyState } from "@/components/app/empty-state";

// The backend does not compute these three aggregates yet (tracked TODO) and may return null until
// it does. A bare 0 here would report a false fact per docs/modules/herd-signals.md ("read failed"
// vs "empty" is the same distinction — an unmeasured value is not a measured zero).
function fmtCount(value: number | null): string {
  return value === null ? "—" : value.toLocaleString("en-IN");
}

// Wire enum -> the label the mock prints in the top-right chip of a gateway card. The gateway
// reports how it is backhauling; anything else (a "4G" chip like the mock's sample card) would be
// a value this deployment has never measured.
const NETWORK_MODE_LABEL: Record<NonNullable<HerdGateway["network_mode"]>, string> = {
  wifi: "Wi-Fi",
  ble: "BLE",
  wifi_ble: "Wi-Fi + BLE",
};

function RadioIcon({ className = "ic" }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24">
      <path d="M5 12.5a7 7 0 0 1 14 0" />
      <path d="M2 9a11 11 0 0 1 20 0" />
      <circle cx="12" cy="17" r="2" />
    </svg>
  );
}

function gatewayName(gateway: HerdGateway): string {
  return gateway.label?.trim() || gateway.gateway_id;
}

// "Mandela block gateway · Channapatna · Mandela 1 - Part 1" in the mock: the human label first,
// then where it is. The shed segment goes through operational_location_display (falling back to
// the shared operationalLocationLabel composer, same as every other herd-signals surface) so a
// partitioned shed never renders as its bare parent name -- an operator sent to "Mandela 1" would
// not know which part. Every part is dropped rather than padded when the backend has not got it.
function locationLine(gateway: HerdGateway): string {
  const shedLocation =
    gateway.operational_location_display ||
    operationalLocationLabel({ shedName: gateway.shed_name, partitionLabel: gateway.partition_label });
  const parts = [gateway.label?.trim() || null, gateway.park_name, shedLocation || null].filter(
    (part): part is string => Boolean(part && part.trim()),
  );
  return parts.length > 0 ? parts.join(" · ") : "No location recorded for this gateway";
}

// Gateway health view (docs/modules/herd-signals.md Section 7: refreshes every 15s — the tab's own
// server read on each visit/refresh; this component is presentation-only).
export function HerdSignalsGateways({ gateways, nowMs }: { gateways: HerdGateway[]; nowMs: number }) {
  if (gateways.length === 0) {
    return (
      <EmptyState
        icon={<RadioIcon />}
        title="No gateways registered yet"
        description="No BLE gateway has posted for this tenant. Confirm a gateway is powered and networked."
      />
    );
  }

  const online = gateways.filter((gateway) => gateway.status === "online").length;
  const isOnline = (gateway: HerdGateway) => gateway.status === "online";

  return (
    <Stack spacing={3}>
      {/* Template card grid (job / tour list cards): header row, location caption, dashed stat strip,
          mono footer. */}
      <Grid container spacing={3}>
        {gateways.map((gateway) => {
          const stats: Array<[string, string, string | undefined, boolean]> = [
            [fmtCount(gateway.tags_seen_in_window), "Tags seen", "15m window", gateway.tags_seen_in_window === null],
            [fmtCount(gateway.distinct_motion_deltas), "Moving", "15m window", gateway.distinct_motion_deltas === null],
            [fmtCount(gateway.packets_received_in_window), "Packets", "15m window", gateway.packets_received_in_window === null],
            [fmtAgo(gateway.last_seen_at, nowMs), "Last packet", undefined, false],
          ];
          return (
            <Grid key={gateway.gateway_id} size={{ xs: 12, md: 6 }}>
              <Card sx={{ p: 3, height: 1, display: "flex", flexDirection: "column", gap: 2 }}>
                <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, flexWrap: "wrap" }}>
                  <Avatar variant="rounded" sx={{ bgcolor: isOnline(gateway) ? "success.lighter" : "error.lighter", color: isOnline(gateway) ? "success.darker" : "error.darker" }}>
                    <Iconify icon="solar:monitor-bold" />
                  </Avatar>
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography variant="subtitle1" noWrap>
                      {gatewayName(gateway)}
                    </Typography>
                    <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>
                      {locationLine(gateway)}
                    </Typography>
                  </Box>
                  <Tag tone={isOnline(gateway) ? "ok" : "dng"}>{isOnline(gateway) ? "Online" : "Offline"}</Tag>
                  {/* Always shown; an em dash when the gateway has not reported how it backhauls yet —
                      never a guessed mode, never a missing chip. */}
                  <Tag tone="mut">{gateway.network_mode ? NETWORK_MODE_LABEL[gateway.network_mode] : "—"}</Tag>
                </Box>

                {!isOnline(gateway) ? (
                  <Alert severity="error">
                    <b>Gateway {gatewayName(gateway)} is offline.</b> No packet for {fmtAgo(gateway.last_seen_at, nowMs)}. A tag
                    heard by ANOTHER gateway keeps reporting normally; only a tag no gateway can hear for 30+ minutes
                    reads as missing signal. Either way that is a statement about the radio path, never a claim that
                    those animals are missing.
                  </Alert>
                ) : null}

                <Box sx={{ display: "grid", gridTemplateColumns: { xs: "repeat(2, 1fr)", sm: "repeat(4, 1fr)" }, borderRadius: "var(--r-lg)", border: 1, borderColor: "divider", borderStyle: "dashed" }}>
                  {stats.map(([value, label, window, pending], index) => (
                    <Box key={label} sx={{ py: 1.5, px: 1, textAlign: "center", borderLeftWidth: { xs: index % 2 ? 1 : 0, sm: index ? 1 : 0 }, borderColor: "divider", borderLeftStyle: "dashed" }}>
                      <Typography variant="h6" title={pending ? "Not computed yet" : undefined}>
                        {value}
                      </Typography>
                      <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>
                        {label}
                      </Typography>
                      {window ? (
                        <Typography variant="caption" component="div" sx={{ color: "text.disabled" }}>
                          {window}
                        </Typography>
                      ) : null}
                    </Box>
                  ))}
                </Box>

                {/* Always printed: an unreported MAC reads as an honest em dash. */}
                <Typography variant="caption" sx={{ mt: "auto", fontFamily: "monospace", color: "text.disabled" }}>
                  {`BLE ${gateway.ble_mac ? fmtBleMac(gateway.ble_mac) : "—"}`}
                  {gateway.wifi_mac ? ` · WiFi ${fmtBleMac(gateway.wifi_mac)}` : ""}
                </Typography>
              </Card>
            </Grid>
          );
        })}
      </Grid>

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 6 }}>
          <Card sx={{ height: 1 }}>
            <CardHeader
              title="Coverage summary"
              action={
                <Label variant="soft" color={online === gateways.length ? "success" : "warning"}>
                  {online} of {gateways.length} posting
                </Label>
              }
              slotProps={{ action: { sx: { alignSelf: "center" } } }}
            />
            <DividedStack sx={{ px: 3, py: 1.5 }}>
              {gateways.map((gateway) => (
                <Box key={gateway.gateway_id} sx={{ py: 1.5, display: "flex", alignItems: "center", gap: 2 }}>
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography variant="subtitle2" sx={{ fontFamily: "monospace" }}>
                      {gateway.gateway_id}
                    </Typography>
                    <Typography variant="caption" sx={{ color: "text.secondary" }}>
                      {fmtCount(gateway.tags_seen_recently)} tags · {fmtAgo(gateway.last_seen_at, nowMs)}
                    </Typography>
                  </Box>
                  <Tag tone={isOnline(gateway) ? "ok" : "dng"}>{isOnline(gateway) ? "Online" : "Offline"}</Tag>
                </Box>
              ))}
            </DividedStack>
          </Card>
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <Card sx={{ height: 1 }}>
            <CardHeader
              title="Battery outlook"
              action={<Label variant="soft">Not computed</Label>}
              slotProps={{ action: { sx: { alignSelf: "center" } } }}
            />
            <EmptyState
              icon={<svg className="ic" viewBox="0 0 24 24">
                <rect x="2" y="7" width="16" height="10" rx="2" />
                <path d="M22 11v2" />
              </svg>}
              title="No battery outlook on this tab yet"
              description="Per-tag battery voltage is on the Live Monitor tab and in each tag's drawer."
            />
          </Card>
        </Grid>
      </Grid>
    </Stack>
  );
}
