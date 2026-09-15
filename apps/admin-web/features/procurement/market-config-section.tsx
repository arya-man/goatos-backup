import { Phone } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ApiResult } from "@/lib/api/server";
import type { MarketCity, MarketConfig, MarketQuestion } from "@/lib/api/market-server";
import {
  addMarketCityAction,
  addMarketQuestionAction,
  setMarketCallTimeAction,
  updateMarketCityAction,
  updateMarketQuestionAction,
} from "./market-actions";
import { MarketConfigForm, type MarketActionOutcomes } from "./market-config-form";

/**
 * Market survey config on Sales Config (maintainer decision 2026-09-14): the cities the
 * procurement director phones each morning and the questions asked in each, with the unit each
 * price is quoted in.
 *
 * EVERY ROW IS ITS OWN FORM, always editable -- there is no edit toggle, no drawer and no query
 * parameter that swaps a row into an "editing" state, so nothing here navigates to change what
 * the screen shows. Save keeps the row's status; Retire / Put back flips it. A retired row stays
 * listed (its history is still readable and it can come back), tagged so it reads as off the
 * list rather than gone.
 *
 * Gated on the page's `market_config_write` control: without it the forms are not rendered and
 * the backend's disabled reason is shown instead. The route behind each form requires the same
 * permission, so the control is the honest label, not the lock.
 *
 * Every form posts through MarketConfigForm and lands IN PLACE: the outcome sentence appears
 * under the row that was saved and the list re-reads itself in the same response. No redirect,
 * no `?notice=`, no scroll to the top (maintainer report 2026-09-15).
 */

/** The outcome sentences every market form may show, resolved once from the page contract. */
function marketOutcomes(pageContract: AdminUiPageContract): MarketActionOutcomes {
  return {
    market_city_saved: copy(pageContract, "action.market_city_saved"),
    market_question_saved: copy(pageContract, "action.market_question_saved"),
    market_call_time_saved: copy(pageContract, "action.market_call_time_saved"),
    market_save_failed: copy(pageContract, "action.market_save_failed"),
    market_duplicate: copy(pageContract, "action.market_duplicate"),
  };
}
export function MarketConfigSection({
  pageContract,
  configResult,
  canConfigure,
}: {
  pageContract: AdminUiPageContract;
  configResult: ApiResult<MarketConfig>;
  canConfigure: boolean;
}) {
  const config = configResult.ok ? configResult.data : { cities: [], questions: [], call_time: "" };
  const outcomes = marketOutcomes(pageContract);
  return (
    <section className="card" aria-label={copy(pageContract, "section.market.aria")}>
      <div className="hd">
        <Phone className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.market.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <Link href="/sales/market-analytics" className="btn sm">
          {copy(pageContract, "link.sales_market_analytics")}
        </Link>
      </div>
      <p className="muted small sales-config-card-copy">{copy(pageContract, "section.market.sub")}</p>

      {!configResult.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{configResult.error.code ?? configResult.error.kind}</b>&nbsp;
          {configResult.error.message || copy(pageContract, "error.load")}
        </div>
      ) : null}

      {!canConfigure ? <div className="note">{copy(pageContract, "disabled.market_config")}</div> : null}

      {/* The ONE time each morning the cards appear on the phone and the reminder goes out. */}
      {/* Maintainer decision 2026-09-14: native time input; value is "HH:MM" IST. */}
      <div className="market-call-time" style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap", marginBottom: 14 }}>
        <div>
          <h4 style={{ margin: "4px 0" }}>{copy(pageContract, "market.call_time.title")}</h4>
          <p className="muted small" style={{ margin: 0 }}>{copy(pageContract, "market.call_time.hint")}</p>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {canConfigure ? (
          <MarketConfigForm action={setMarketCallTimeAction} outcomes={outcomes} style={{ display: "flex", gap: 8, alignItems: "center" }} data-market-call-time="">
            <input type="time" name="call_time" defaultValue={config.call_time} required aria-label={copy(pageContract, "market.call_time.title")} />
            <button type="submit" className="btn sm primary">
              {copy(pageContract, "market.action.save_call_time")}
            </button>
          </MarketConfigForm>
        ) : (
          <Tag tone="info">{config.call_time}</Tag>
        )}
      </div>

      <div className="grid g2" style={{ gap: 16, alignItems: "start" }}>
        <div>
          <h4 style={{ margin: "4px 0" }}>{copy(pageContract, "market.cities.title")}</h4>
          <p className="muted small">{copy(pageContract, "market.cities.hint")}</p>
          {config.cities.length === 0 ? (
            <div className="empty">{copy(pageContract, "market.empty.cities")}</div>
          ) : (
            <ul className="market-config-list" style={{ listStyle: "none", padding: 0, margin: 0 }}>
              {config.cities.map((city) => (
                <li key={city.id} style={{ padding: "6px 0", borderBottom: "1px solid var(--line)" }}>
                  <CityRow city={city} pageContract={pageContract} canConfigure={canConfigure} outcomes={outcomes} />
                </li>
              ))}
            </ul>
          )}
          {canConfigure ? (
            <MarketConfigForm action={addMarketCityAction} outcomes={outcomes} className="market-config-add" style={{ display: "flex", gap: 8, marginTop: 10 }}>
              <input
                name="name"
                required
                maxLength={80}
                placeholder={copy(pageContract, "market.field.city_name")}
                aria-label={copy(pageContract, "market.field.city_name")}
                style={{ flex: 1 }}
              />
              <button type="submit" className="btn sm primary">
                {copy(pageContract, "market.action.add_city")}
              </button>
            </MarketConfigForm>
          ) : null}
        </div>

        <div>
          <h4 style={{ margin: "4px 0" }}>{copy(pageContract, "market.questions.title")}</h4>
          <p className="muted small">{copy(pageContract, "market.questions.hint")}</p>
          {config.questions.length === 0 ? (
            <div className="empty">{copy(pageContract, "market.empty.questions")}</div>
          ) : (
            <ul className="market-config-list" style={{ listStyle: "none", padding: 0, margin: 0 }}>
              {config.questions.map((question) => (
                <li key={question.id} style={{ padding: "6px 0", borderBottom: "1px solid var(--line)" }}>
                  <QuestionRow question={question} pageContract={pageContract} canConfigure={canConfigure} outcomes={outcomes} />
                </li>
              ))}
            </ul>
          )}
          {canConfigure ? (
            <MarketConfigForm action={addMarketQuestionAction} outcomes={outcomes} className="market-config-add" style={{ display: "flex", gap: 8, marginTop: 10 }}>
              <input
                name="label"
                required
                maxLength={80}
                placeholder={copy(pageContract, "market.field.question_label")}
                aria-label={copy(pageContract, "market.field.question_label")}
                style={{ flex: 2 }}
              />
              <input
                name="unit_label"
                required
                maxLength={24}
                placeholder={copy(pageContract, "market.field.unit_label")}
                aria-label={copy(pageContract, "market.field.unit_label")}
                style={{ flex: 1 }}
              />
              <button type="submit" className="btn sm primary">
                {copy(pageContract, "market.action.add_question")}
              </button>
            </MarketConfigForm>
          ) : null}
        </div>
      </div>
      <p className="muted small" style={{ marginTop: 10 }}>
        {copy(pageContract, "market.retired_note")}
      </p>
    </section>
  );
}

function StatusTag({ status, pageContract }: { status: string; pageContract: AdminUiPageContract }) {
  return status === "active" ? (
    <Tag tone="ok">{copy(pageContract, "market.status.active")}</Tag>
  ) : (
    <Tag tone="mut">{copy(pageContract, "market.status.retired")}</Tag>
  );
}

function CityRow({
  city,
  pageContract,
  canConfigure,
  outcomes,
}: {
  city: MarketCity;
  pageContract: AdminUiPageContract;
  canConfigure: boolean;
  outcomes: MarketActionOutcomes;
}) {
  if (!canConfigure) {
    return (
      <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
        <span style={{ flex: 1 }}>{city.name}</span>
        <StatusTag status={city.status} pageContract={pageContract} />
      </div>
    );
  }
  const flipped = city.status === "active" ? "retired" : "active";
  return (
    <MarketConfigForm action={updateMarketCityAction} outcomes={outcomes} style={{ display: "flex", gap: 8, alignItems: "center" }} data-market-city={city.id}>
      <input type="hidden" name="city_id" value={city.id} />
      <input name="name" defaultValue={city.name} required maxLength={80} aria-label={copy(pageContract, "market.field.city_name")} style={{ flex: 1 }} />
      <StatusTag status={city.status} pageContract={pageContract} />
      <button type="submit" name="status" value={city.status} className="btn sm">
        {copy(pageContract, "market.action.save")}
      </button>
      <button type="submit" name="status" value={flipped} className="btn sm">
        {city.status === "active" ? copy(pageContract, "market.action.retire") : copy(pageContract, "market.action.restore")}
      </button>
    </MarketConfigForm>
  );
}

function QuestionRow({
  question,
  pageContract,
  canConfigure,
  outcomes,
}: {
  question: MarketQuestion;
  pageContract: AdminUiPageContract;
  canConfigure: boolean;
  outcomes: MarketActionOutcomes;
}) {
  if (!canConfigure) {
    return (
      <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
        <span style={{ flex: 2 }}>{question.label}</span>
        <span className="muted" style={{ flex: 1 }}>{question.unit_label}</span>
        <StatusTag status={question.status} pageContract={pageContract} />
      </div>
    );
  }
  const flipped = question.status === "active" ? "retired" : "active";
  return (
    <MarketConfigForm action={updateMarketQuestionAction} outcomes={outcomes} style={{ display: "flex", gap: 8, alignItems: "center" }} data-market-question={question.id}>
      <input type="hidden" name="question_id" value={question.id} />
      <input name="label" defaultValue={question.label} required maxLength={80} aria-label={copy(pageContract, "market.field.question_label")} style={{ flex: 2 }} />
      <input name="unit_label" defaultValue={question.unit_label} required maxLength={24} aria-label={copy(pageContract, "market.field.unit_label")} style={{ flex: 1 }} />
      <StatusTag status={question.status} pageContract={pageContract} />
      <button type="submit" name="status" value={question.status} className="btn sm">
        {copy(pageContract, "market.action.save")}
      </button>
      <button type="submit" name="status" value={flipped} className="btn sm">
        {question.status === "active" ? copy(pageContract, "market.action.retire") : copy(pageContract, "market.action.restore")}
      </button>
    </MarketConfigForm>
  );
}
