import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtStaleness } from "./format";
import type { HerdSignalsGateway } from "./types";

export function HerdSignalsGateways({ gateways, pageContract, nowMs }: { gateways: HerdSignalsGateway[]; pageContract: AdminUiPageContract; nowMs: number }) {
  if (gateways.length === 0) {
    return <div className="hs-empty">{copy(pageContract, "gateways.empty", "No gateways are registered yet.")}</div>;
  }
  return (
    <div className="hs-gateway-grid">
      {gateways.map((gateway) => (
        <div key={gateway.gateway_id} className={`card hs-gwcard hs-gw-${gateway.status}`}>
          <div className="hd">
            <span className="hs-gw-label">{gateway.label}</span>
            <span className={`hs-badge hs-gw-status-${gateway.status}`}>{gateway.status}</span>
          </div>
          <div className="bd hs-gwstats">
            <div>{gateway.park_name}{gateway.shed_name ? ` · ${gateway.shed_name}` : ""}</div>
            <div className="muted">{gateway.network_mode}</div>
            <div className="hs-gwstats-row">
              <span>{gateway.tags_seen_recently} seen</span>
              <span>{gateway.weak_tags} weak</span>
              <span>{gateway.unmapped_tags} unmapped</span>
            </div>
            <div className="muted">Last seen {fmtStaleness(gateway.last_seen_at, nowMs)}</div>
          </div>
        </div>
      ))}
    </div>
  );
}
