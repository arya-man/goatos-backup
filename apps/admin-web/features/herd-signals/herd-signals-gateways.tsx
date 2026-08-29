import { Tag } from "@/components/ui-primitives";
import type { HerdGateway } from "@/lib/api/herd-signals";
import { operationalLocationLabel } from "@/lib/operational-location";
import { fmtAgo, fmtBleMac } from "./format";

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
      <div className="empty">
        <div className="eicon">
          <RadioIcon />
        </div>
        <h4>No gateways registered yet</h4>
        <p>No BLE gateway has posted for this tenant. Confirm a gateway is powered and networked.</p>
      </div>
    );
  }

  const online = gateways.filter((gateway) => gateway.status === "online").length;

  return (
    <>
      <div className="grid2">
        {gateways.map((gateway) => (
          <div key={gateway.gateway_id} className="gwcard">
            <div className="gwh">
              <RadioIcon />
              <b>{gatewayName(gateway)}</b>
              <Tag tone={gateway.status === "online" ? "ok" : "dng"}>
                {gateway.status === "online" ? "Online" : "Offline"}
              </Tag>
              <div className="sp" />
              {/* The mock always shows this chip; an absent one silently drops a field the reader
                  expects to see. Em dash when the gateway has not reported how it backhauls yet —
                  never a guessed mode, but never a missing chip either. */}
              <Tag tone="mut">{gateway.network_mode ? NETWORK_MODE_LABEL[gateway.network_mode] : "—"}</Tag>
            </div>

            <div className="muted small">{locationLine(gateway)}</div>

            {gateway.status !== "online" ? (
              <div className="banner dng" style={{ margin: "10px 0 0" }}>
                <svg className="ic" viewBox="0 0 24 24">
                  <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
                  <path d="M12 9v4" />
                  <path d="M12 17h.01" />
                </svg>
                <div>
                  <b>Gateway {gatewayName(gateway)} is offline.</b> No packet for{" "}
                  {fmtAgo(gateway.last_seen_at, nowMs)}. A tag heard by ANOTHER gateway keeps
                  reporting normally; only a tag no gateway can hear for 30+ minutes reads as missing
                  signal. Either way that is a statement about the radio path, never a claim that
                  those animals are missing.
                </div>
              </div>
            ) : null}

            <div className="gwstats">
              <div>
                <div className="v" title={gateway.tags_seen_in_window === null ? "Not computed yet" : undefined}>
                  {fmtCount(gateway.tags_seen_in_window)}
                </div>
                <div className="l">Tags seen<br /><small>15m window</small></div>
              </div>
              <div>
                <div className="v" title={gateway.distinct_motion_deltas === null ? "Not computed yet" : undefined}>
                  {fmtCount(gateway.distinct_motion_deltas)}
                </div>
                <div className="l">Moving<br /><small>15m window</small></div>
              </div>
              <div>
                <div className="v" title={gateway.packets_received_in_window === null ? "Not computed yet" : undefined}>
                  {fmtCount(gateway.packets_received_in_window)}
                </div>
                <div className="l">Packets<br /><small>15m window</small></div>
              </div>
              <div>
                <div className="v">{fmtAgo(gateway.last_seen_at, nowMs)}</div>
                <div className="l">Last packet</div>
              </div>
            </div>

            {/* The mock always prints this footer line. Matching that structurally means an
                unreported MAC reads as an honest em dash, not a vanished line the reader has no
                way to tell apart from "this card has no footer". */}
            <div className="mono faint small">
              {`BLE ${gateway.ble_mac ? fmtBleMac(gateway.ble_mac) : "—"}`}
              {gateway.wifi_mac ? ` · WiFi ${fmtBleMac(gateway.wifi_mac)}` : ""}
            </div>
          </div>
        ))}
      </div>

      <div className="grid2" style={{ marginTop: 14 }}>
        <div className="card">
          <div className="hd">
            <RadioIcon />
            <h3>Coverage summary</h3>
            <div className="sp" style={{ flex: 1 }} />
            <span className="small faint">
              {online} of {gateways.length} posting
            </span>
          </div>
          <div className="bd">
            {gateways.map((gateway) => (
              <div
                key={gateway.gateway_id}
                style={{ display: "flex", alignItems: "center", gap: 9, padding: "7px 0", borderBottom: "1px solid var(--line2)" }}
              >
                <RadioIcon className="ic" />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <b className="mono">{gateway.gateway_id}</b>
                  <div className="faint small">
                    {fmtCount(gateway.tags_seen_recently)} tags · {fmtAgo(gateway.last_seen_at, nowMs)}
                  </div>
                </div>
                <Tag tone={gateway.status === "online" ? "ok" : "dng"}>
                  {gateway.status === "online" ? "Online" : "Offline"}
                </Tag>
              </div>
            ))}
          </div>
        </div>

        <div className="card">
          <div className="hd">
            <svg className="ic" viewBox="0 0 24 24">
              <rect x="2" y="7" width="16" height="10" rx="2" />
              <path d="M22 11v2" />
            </svg>
            <h3>Battery outlook</h3>
            <div className="sp" style={{ flex: 1 }} />
            <Tag tone="mut">Not computed</Tag>
          </div>
          <div className="bd">
            <div className="empty" style={{ padding: "26px 12px" }}>
              <div className="eicon">
                <svg className="ic" viewBox="0 0 24 24">
                  <rect x="2" y="7" width="16" height="10" rx="2" />
                  <path d="M22 11v2" />
                </svg>
              </div>
              <h4>No battery outlook on this tab yet</h4>
              <p>
                Battery voltage arrives per tag, not per gateway, and the gateway read used by this
                tab does not carry it. Per-tag battery voltage and its recent direction are on the
                Live Monitor tab and in each tag&apos;s detail drawer.
              </p>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
