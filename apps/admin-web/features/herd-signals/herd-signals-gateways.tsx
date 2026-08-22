import { Tag } from "@/components/ui-primitives";
import type { HerdGateway } from "@/lib/api/herd-signals";
import { fmtAgo } from "./format";

// The backend does not compute these three aggregates yet (tracked TODO) and may return null until
// it does. A bare 0 here would report a false fact per docs/modules/herd-signals.md ("read failed"
// vs "empty" is the same distinction — an unmeasured value is not a measured zero).
function fmtCount(value: number | null): string {
  return value === null ? "—" : value.toLocaleString("en-IN");
}

// Gateway health view (docs/modules/herd-signals.md Section 7: refreshes every 15s — the tab's own
// server read on each visit/refresh; this component is presentation-only).
export function HerdSignalsGateways({ gateways, nowMs }: { gateways: HerdGateway[]; nowMs: number }) {
  if (gateways.length === 0) {
    return (
      <div className="empty">
        <div className="eicon">
          <svg className="ic" viewBox="0 0 24 24">
            <path d="M5 12.5a7 7 0 0 1 14 0" />
            <path d="M2 9a11 11 0 0 1 20 0" />
            <circle cx="12" cy="17" r="2" />
          </svg>
        </div>
        <h4>No gateways registered yet</h4>
        <p>No BLE gateway has posted for this tenant. Confirm a gateway is powered and networked.</p>
      </div>
    );
  }
  return (
    <div className="grid2">
      {gateways.map((gateway) => (
        <div key={gateway.gateway_id} className="gwcard">
          <div className="gwh">
            <b>{gateway.label || gateway.gateway_id}</b>
            <span className="sp" style={{ flex: 1 }} />
            <Tag tone={gateway.status === "online" ? "ok" : "dng"}>{gateway.status === "online" ? "Online" : "Offline"}</Tag>
          </div>
          {gateway.status !== "online" ? (
            <div className="banner warn" style={{ margin: "0 0 10px" }}>
              <svg className="ic" viewBox="0 0 24 24">
                <circle cx="12" cy="12" r="9" />
                <path d="M12 7v5l3 2" />
              </svg>
              <div>
                <b>Gateway offline.</b> Its tags read as missing signal below — that is a statement
                about the radio path, never a claim that those animals are missing.
              </div>
            </div>
          ) : null}
          <div className="muted small">
            {gateway.park_name || "—"}
            {gateway.shed_name ? ` · ${gateway.shed_name}` : ""}
          </div>
          <div className="muted small mono">{gateway.network_mode || "—"}</div>
          <div className="gwstats">
            <div>
              <div className="v" title={gateway.tags_seen_recently === null ? "Not available yet" : undefined}>
                {fmtCount(gateway.tags_seen_recently)}
              </div>
              <div className="l">Seen</div>
            </div>
            <div>
              <div className="v" title={gateway.weak_tags === null ? "Not available yet" : undefined}>
                {fmtCount(gateway.weak_tags)}
              </div>
              <div className="l">Weak</div>
            </div>
            <div>
              <div className="v" title={gateway.unmapped_tags === null ? "Not available yet" : undefined}>
                {fmtCount(gateway.unmapped_tags)}
              </div>
              <div className="l">Unmapped</div>
            </div>
          </div>
          <div className="muted small">Last seen {fmtAgo(gateway.last_seen_at, nowMs)}</div>
        </div>
      ))}
    </div>
  );
}
