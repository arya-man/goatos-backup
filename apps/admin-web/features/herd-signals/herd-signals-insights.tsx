import type { HerdInsightCard } from "@/lib/api/herd-signals";

const TYPE_LABEL: Record<HerdInsightCard["signal_type"], string> = {
  direct: "Direct",
  derived: "Derived",
  correlated: "Correlated",
  inferred: "Inferred",
};

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
              <div className="ih">{card.label}</div>
              <div className="iv">
                {card.value ?? "—"} {card.unit ? <small style={{ fontSize: 13, color: "var(--faint)" }}>{card.unit}</small> : null}
              </div>
              <div className="if">
                {TYPE_LABEL[card.signal_type]} · {card.formula}
              </div>
              {card.caveat ? <div className="idis">{card.caveat}</div> : null}
            </div>
          ))}
        </div>
      )}
    </>
  );
}
