import { Phone } from "lucide-react";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";

import Link from "@/components/no-prefetch-link";
import { TimeField } from "@/components/app/time-field";
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
import Alert from "@mui/material/Alert";
import { EmptyState } from "@/components/app/empty-state";
import { salesErrorText } from "./sales-error";

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

      {/* Body padding comes from the card's own .bd; the rows/blocks are spaced by the market-config-* rules. */}
      <div className="bd market-config-body">
      {!configResult.ok ? (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          {salesErrorText(configResult.error, copy(pageContract, "market.error.load"))}
        </Alert>
      ) : null}
      {/* A failed read shows the error and nothing else; otherwise the empty set-up invites the desk
          to re-enter a set-up that exists. */}
      {!configResult.ok ? null : (
      <>

      {!canConfigure ? <div className="note">{copy(pageContract, "disabled.market_config")}</div> : null}

      {/* The ONE time each morning the cards appear on the phone and the reminder goes out. */}
      {/* Maintainer decision 2026-09-14: the value posted is "HH:MM" IST — unchanged; the kit
          TimeField posts exactly that through its hidden input, so behaviour is the same. */}
      <div className="market-call-time">
        <div>
          <h4 className="market-config-h4">{copy(pageContract, "market.call_time.title")}</h4>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {canConfigure ? (
          <MarketConfigForm action={setMarketCallTimeAction} outcomes={outcomes} className="market-config-row" data-market-call-time="">
            <TimeField
              name="call_time"
              defaultValue={config.call_time}
              required
              ariaLabel={copy(pageContract, "market.call_time.title")}
              hourLabel={copy(pageContract, "market.call_time.hour", "Hour")}
              minuteLabel={copy(pageContract, "market.call_time.minute", "Minute")}
            />
            <Button type="submit" size="small" variant="contained" color="primary">
              {copy(pageContract, "market.action.save_call_time")}
            </Button>
          </MarketConfigForm>
        ) : (
          <Tag tone="info">{config.call_time}</Tag>
        )}
      </div>

      <div className="grid g2 market-config-columns">
        <div className="market-config-column">
          <h4 className="market-config-h4">
            {copy(pageContract, "market.cities.title")}
            <span className="market-config-count">{config.cities.filter((c) => c.status === "active").length}</span>
          </h4>
          {config.cities.length === 0 ? (
            <EmptyState title={copy(pageContract, "market.empty.cities")} />
          ) : (
            <ul className="market-config-list">
              {config.cities.map((city) => (
                <li key={city.id} className={city.status === "active" ? undefined : "is-retired"}>
                  <CityRow city={city} pageContract={pageContract} canConfigure={canConfigure} outcomes={outcomes} />
                </li>
              ))}
            </ul>
          )}
          {canConfigure ? (
            <MarketConfigForm action={addMarketCityAction} outcomes={outcomes} className="market-config-add">
              <TextField
                size="small"
                name="name"
                required
                placeholder={copy(pageContract, "market.field.city_name")}
                slotProps={{ htmlInput: { maxLength: 80, "aria-label": copy(pageContract, "market.field.city_name") } }}
                sx={{ flex: 1 }}
              />
              <Button type="submit" size="small" variant="contained" color="primary">
                {copy(pageContract, "market.action.add_city")}
              </Button>
            </MarketConfigForm>
          ) : null}
        </div>

        <div className="market-config-column">
          <h4 className="market-config-h4">
            {copy(pageContract, "market.questions.title")}
            <span className="market-config-count">{config.questions.filter((q) => q.status === "active").length}</span>
          </h4>
          {config.questions.length === 0 ? (
            <EmptyState title={copy(pageContract, "market.empty.questions")} />
          ) : (
            <ul className="market-config-list">
              {config.questions.map((question) => (
                <li key={question.id} className={question.status === "active" ? undefined : "is-retired"}>
                  <QuestionRow question={question} pageContract={pageContract} canConfigure={canConfigure} outcomes={outcomes} />
                </li>
              ))}
            </ul>
          )}
          {canConfigure ? (
            <MarketConfigForm action={addMarketQuestionAction} outcomes={outcomes} className="market-config-add">
              <TextField
                size="small"
                name="label"
                required
                placeholder={copy(pageContract, "market.field.question_label")}
                slotProps={{ htmlInput: { maxLength: 80, "aria-label": copy(pageContract, "market.field.question_label") } }}
                sx={{ flex: 2 }}
              />
              <TextField
                size="small"
                name="unit_label"
                required
                placeholder={copy(pageContract, "market.field.unit_label")}
                slotProps={{ htmlInput: { maxLength: 24, "aria-label": copy(pageContract, "market.field.unit_label") } }}
                sx={{ flex: 1 }}
              />
              <Button type="submit" size="small" variant="contained" color="primary">
                {copy(pageContract, "market.action.add_question")}
              </Button>
            </MarketConfigForm>
          ) : null}
        </div>
      </div>
      </>
      )}
      </div>
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
      <div className="market-config-line">
        <span style={{ flex: 1 }}>{city.name}</span>
        <StatusTag status={city.status} pageContract={pageContract} />
      </div>
    );
  }
  const flipped = city.status === "active" ? "retired" : "active";
  return (
    <MarketConfigForm action={updateMarketCityAction} outcomes={outcomes} className="market-config-row" data-market-city={city.id}>
      <input type="hidden" name="city_id" value={city.id} />
      <TextField
        size="small"
        name="name"
        defaultValue={city.name}
        required
        slotProps={{ htmlInput: { maxLength: 80, "aria-label": copy(pageContract, "market.field.city_name") } }}
        sx={{ flex: 1 }}
      />
      <StatusTag status={city.status} pageContract={pageContract} />
      <Button type="submit" name="status" value={city.status} size="small" variant="outlined">
        {copy(pageContract, "market.action.save")}
      </Button>
      <Button type="submit" name="status" value={flipped} size="small" variant="outlined">
        {city.status === "active" ? copy(pageContract, "market.action.retire") : copy(pageContract, "market.action.restore")}
      </Button>
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
      <div className="market-config-line">
        <span style={{ flex: 2 }}>{question.label}</span>
        <span className="muted" style={{ flex: 1 }}>{question.unit_label}</span>
        <StatusTag status={question.status} pageContract={pageContract} />
      </div>
    );
  }
  const flipped = question.status === "active" ? "retired" : "active";
  return (
    <MarketConfigForm action={updateMarketQuestionAction} outcomes={outcomes} className="market-config-row" data-market-question={question.id}>
      <input type="hidden" name="question_id" value={question.id} />
      <TextField
        size="small"
        name="label"
        defaultValue={question.label}
        required
        slotProps={{ htmlInput: { maxLength: 80, "aria-label": copy(pageContract, "market.field.question_label") } }}
        sx={{ flex: 2 }}
      />
      <TextField
        size="small"
        name="unit_label"
        defaultValue={question.unit_label}
        required
        slotProps={{ htmlInput: { maxLength: 24, "aria-label": copy(pageContract, "market.field.unit_label") } }}
        sx={{ flex: 1 }}
      />
      <StatusTag status={question.status} pageContract={pageContract} />
      <Button type="submit" name="status" value={question.status} size="small" variant="outlined">
        {copy(pageContract, "market.action.save")}
      </Button>
      <Button type="submit" name="status" value={flipped} size="small" variant="outlined">
        {question.status === "active" ? copy(pageContract, "market.action.retire") : copy(pageContract, "market.action.restore")}
      </Button>
    </MarketConfigForm>
  );
}
