import type { HerdInsightCard, HerdSignalType } from "@/lib/api/herd-signals";

const TYPE_LABEL: Record<HerdSignalType, string> = {
  direct: "Direct",
  derived: "Derived",
  correlated: "Correlated",
  inferred: "Inferred",
};

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
          <b>What the tag actually reports:</b> tag ID, BLE MAC, RSSI, battery voltage, tag
          temperature, a cumulative motion counter, sensor-OK bits, gateway ID and timestamps. It
          does not detect eating, rumination, posture, walking, fever, body temperature or disease.
          Every card below is labelled Direct, Derived, Correlated or Inferred.
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
    </>
  );
}
