import type { HerdInsightCard, HerdSignalType } from "@/lib/api/herd-signals";

const TYPE_LABEL: Record<HerdSignalType, string> = {
  direct: "Direct",
  derived: "Derived",
  correlated: "Correlated",
  inferred: "Inferred",
};

// The backend now ports the label (Title Case), formula (plain-English, not SQL) and signal_type
// (badge tier) verbatim from the design reference's card set card-for-card (see
// backend/internal/herdsignals/app/service.go GetInsights) -- this component renders those fields
// as returned, with no frontend copy override, so backend and screen can never drift.
//
// One icon per PROVENANCE tier, not per card key. The mock gives each of its sample cards its own
// glyph; our card list is backend-owned and open-ended, so keying the glyph off `signal_type` is
// the only mapping that stays honest when the backend adds a card this file has never seen.
function TierIcon({ tier }: { tier: HerdSignalType }) {
  if (tier === "direct") {
    return (
      <svg className="ic sm" viewBox="0 0 24 24">
        <path d="M5 12.5a7 7 0 0 1 14 0" />
        <path d="M2 9a11 11 0 0 1 20 0" />
        <circle cx="12" cy="17" r="2" />
      </svg>
    );
  }
  if (tier === "correlated") {
    return (
      <svg className="ic sm" viewBox="0 0 24 24">
        <path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7" />
        <path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7L12.2 19" />
      </svg>
    );
  }
  if (tier === "inferred") {
    return (
      <svg className="ic sm" viewBox="0 0 24 24">
        <circle cx="12" cy="12" r="9" />
        <path d="M9.5 9.5a2.6 2.6 0 0 1 5 .8c0 1.7-2.5 2.2-2.5 3.7" />
        <path d="M12 17h.01" />
      </svg>
    );
  }
  return (
    <svg className="ic sm" viewBox="0 0 24 24">
      <path d="M3 12h4l3 8 4-16 3 8h4" />
    </svg>
  );
}

export function HerdSignalsInsights({ cards }: { cards: HerdInsightCard[] }) {
  return (
    <>
      <div className="banner info">
        <svg className="ic" viewBox="0 0 24 24">
          <circle cx="12" cy="12" r="9" />
          <path d="M12 16v-4" />
          <path d="M12 8h.01" />
        </svg>
        <div>
          <b>What the tag actually reports:</b> tag ID, BLE MAC, RSSI, battery mV, tag temperature,
          a cumulative motion counter, sensor-OK bits, gateway ID and timestamps. It does not
          detect eating, rumination, posture, walking, fever, body temperature or disease.
          Everything below is labelled Direct, Derived, Correlated or Inferred.
        </div>
      </div>
      {cards.length === 0 ? (
        <div className="empty">
          <div className="eicon">
            <svg className="ic" viewBox="0 0 24 24">
              <path d="M3 12h4l3 8 4-16 3 8h4" />
            </svg>
          </div>
          <h4>No insight cards yet</h4>
          <p>Insights are computed from activity windows written as packets arrive.</p>
        </div>
      ) : (
        <div className="grid2">
          {cards.map((card) => (
            <div key={card.key} className="insight">
              <div className="ih">
                <TierIcon tier={card.signal_type} />
                <span>{card.label}</span>
                <span className={`srcl ${card.signal_type}`}>{TYPE_LABEL[card.signal_type]}</span>
              </div>
              {/* An uncomputed card renders "—", never a 0 that reads as a measured count. */}
              <div className="iv">
                {card.value ?? "—"}
                {card.value !== null && card.unit ? (
                  <small style={{ fontSize: 13, color: "var(--faint)", marginLeft: 4 }}>{card.unit}</small>
                ) : null}
              </div>
              <div className="if">{card.formula}</div>
              {card.caveat ? <div className="idis">{card.caveat}</div> : null}
            </div>
          ))}
        </div>
      )}
      <div className="card" style={{ marginTop: 14 }}>
        <div className="hd">
          <svg className="ic" viewBox="0 0 24 24">
            <rect x="5" y="2" width="14" height="20" rx="2" />
            <path d="M12 18h.01" />
          </svg>
          <h3>Vaccination proof — BLE scan flow (design)</h3>
          <div className="sp" />
          <span className="tag t-mut">Not built</span>
        </div>
        <div className="bd">
          <ol className="muted" style={{ margin: "0 0 10px", paddingLeft: 18, fontSize: 12.5, lineHeight: 1.8 }}>
            <li>Operator opens the vaccination task.</li>
            <li>App starts a nearby BLE scan, tags sorted by RSSI.</li>
            <li>Operator brings the phone close to the ear tag.</li>
            <li>
              App highlights the strongest <b>mapped</b> tag.
            </li>
            <li>Operator confirms animal ↔ tag.</li>
            <li>Camera opens.</li>
            <li>Proof stores animal_id, BLE tag ID, BLE MAC, RSSI, scanner source, timestamp, video proof ID.</li>
          </ol>
          <div className="banner dng" style={{ margin: "0 0 10px" }}>
            <svg className="ic" viewBox="0 0 24 24">
              <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
              <path d="M12 9v4" />
              <path d="M12 17h.01" />
            </svg>
            <div>
              <b>Safety gate.</b> The camera never auto-opens because a BLE tag is merely visible.
              Require strongest RSSI ≥ −60 dBm <b>and</b> ≥ 8 dB clear of the next strongest, or an
              explicit manual operator confirmation.
            </div>
          </div>
          <div className="tagrow" style={{ display: "flex", gap: 7, flexWrap: "wrap" }}>
            <span className="tag t-ok">Nearest tag locked</span>
            <span className="tag t-warn">Multiple tags nearby</span>
            <span className="tag t-warn">Move closer</span>
            <span className="tag t-dng">Tag not mapped</span>
            <span className="tag t-dng">Bluetooth permission required</span>
            <span className="tag t-mut">Scanner unavailable — manual verification path</span>
          </div>
        </div>
      </div>
    </>
  );
}
