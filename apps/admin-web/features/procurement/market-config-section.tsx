import Button from "@mui/material/Button";
import { DividedStack } from "@/components/app/divided-stack";
import TextField from "@mui/material/TextField";

import Link from "@/components/no-prefetch-link";
import { TimeField } from "@/components/app/time-field";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Paper from "@mui/material/Paper";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
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
  const activeCities = config.cities.filter((c) => c.status === "active").length;
  const activeQuestions = config.questions.filter((q) => q.status === "active").length;
  return (
    // Template account-settings card: CardHeader (title + Market analytics link), body on p: 3.
    <Card component="section" aria-label={copy(pageContract, "section.market.aria")}>
      <CardHeader
        title={copy(pageContract, "section.market.title")}
        action={
          <Button component={Link} href="/sales/market-analytics" size="small" color="inherit" endIcon={<Iconify icon="eva:arrow-ios-forward-fill" />}>
            {copy(pageContract, "link.sales_market_analytics")}
          </Button>
        }
      />
      <Stack spacing={3} sx={{ p: 3 }}>
      {!configResult.ok ? (
        <Alert severity="error">
          {salesErrorText(configResult.error, copy(pageContract, "market.error.load"))}
        </Alert>
      ) : null}
      {/* A failed read shows the error and nothing else; otherwise the empty set-up invites the desk
          to re-enter a set-up that exists. */}
      {!configResult.ok ? null : (
      <>

      {!canConfigure ? <Alert severity="info">{copy(pageContract, "disabled.market_config")}</Alert> : null}

      {/* The ONE time each morning the cards appear on the phone and the reminder goes out. */}
      {/* Maintainer decision 2026-09-14: the value posted is "HH:MM" IST — unchanged; the kit
          TimeField posts exactly that through its hidden input, so behaviour is the same. */}
      <Paper variant="outlined" sx={{ p: 2, borderStyle: "dashed", display: "flex", flexWrap: "wrap", alignItems: "center", gap: 2 }}>
        <Typography variant="subtitle2" sx={{ flexGrow: 1 }}>{copy(pageContract, "market.call_time.title")}</Typography>
        {canConfigure ? (
          <MarketConfigForm action={setMarketCallTimeAction} outcomes={outcomes} data-market-call-time="">
            <TimeField
              name="call_time"
              defaultValue={config.call_time}
              required
              ariaLabel={copy(pageContract, "market.call_time.title")}
              hourLabel={copy(pageContract, "market.call_time.hour", "Hour")}
              minuteLabel={copy(pageContract, "market.call_time.minute", "Minute")}
            />
            <Button type="submit" variant="contained" color="primary">
              {copy(pageContract, "market.action.save_call_time")}
            </Button>
          </MarketConfigForm>
        ) : (
          <Label variant="soft" color="info">{config.call_time}</Label>
        )}
      </Paper>

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 6 }} sx={{ minWidth: 0 }}>
          <Stack direction="row" sx={{ alignItems: "center", gap: 1, mb: 1.5 }}>
            <Typography variant="subtitle1">{copy(pageContract, "market.cities.title")}</Typography>
            <Label variant="soft" color={activeCities ? "info" : "default"}>{activeCities}</Label>
          </Stack>
          {config.cities.length === 0 ? (
            <EmptyState title={copy(pageContract, "market.empty.cities")} />
          ) : (
            <DividedStack spacing={1.5}>
              {config.cities.map((city) => (
                <Box key={city.id} sx={{ opacity: city.status === "active" ? 1 : 0.64 }}>
                  <CityRow city={city} pageContract={pageContract} canConfigure={canConfigure} outcomes={outcomes} />
                </Box>
              ))}
            </DividedStack>
          )}
          {canConfigure ? (
            <Box sx={{ mt: 2.5, pt: 2.5, borderTop: "1px dashed", borderColor: "divider" }}>
            <MarketConfigForm action={addMarketCityAction} outcomes={outcomes}>
              <TextField
                size="small"
                name="name"
                required
                label={copy(pageContract, "market.field.city_name")}
                slotProps={{ htmlInput: { maxLength: 80 } }}
                sx={{ flex: "1 1 200px" }}
              />
              <Button type="submit" variant="contained" color="primary" startIcon={<Iconify icon="mingcute:add-line" />}>
                {copy(pageContract, "market.action.add_city")}
              </Button>
            </MarketConfigForm>
            </Box>
          ) : null}
        </Grid>

        <Grid size={{ xs: 12, md: 6 }} sx={{ minWidth: 0 }}>
          <Stack direction="row" sx={{ alignItems: "center", gap: 1, mb: 1.5 }}>
            <Typography variant="subtitle1">{copy(pageContract, "market.questions.title")}</Typography>
            <Label variant="soft" color={activeQuestions ? "info" : "default"}>{activeQuestions}</Label>
          </Stack>
          {config.questions.length === 0 ? (
            <EmptyState title={copy(pageContract, "market.empty.questions")} />
          ) : (
            <DividedStack spacing={1.5}>
              {config.questions.map((question) => (
                <Box key={question.id} sx={{ opacity: question.status === "active" ? 1 : 0.64 }}>
                  <QuestionRow question={question} pageContract={pageContract} canConfigure={canConfigure} outcomes={outcomes} />
                </Box>
              ))}
            </DividedStack>
          )}
          {canConfigure ? (
            <Box sx={{ mt: 2.5, pt: 2.5, borderTop: "1px dashed", borderColor: "divider" }}>
            <MarketConfigForm action={addMarketQuestionAction} outcomes={outcomes}>
              <TextField
                size="small"
                name="label"
                required
                label={copy(pageContract, "market.field.question_label")}
                slotProps={{ htmlInput: { maxLength: 80 } }}
                sx={{ flex: "2 1 200px" }}
              />
              <TextField
                size="small"
                name="unit_label"
                required
                label={copy(pageContract, "market.field.unit_label")}
                slotProps={{ htmlInput: { maxLength: 24 } }}
                sx={{ flex: "1 1 120px" }}
              />
              <Button type="submit" variant="contained" color="primary" startIcon={<Iconify icon="mingcute:add-line" />}>
                {copy(pageContract, "market.action.add_question")}
              </Button>
            </MarketConfigForm>
            </Box>
          ) : null}
        </Grid>
      </Grid>
      </>
      )}
      </Stack>
    </Card>
  );
}

function StatusTag({ status, pageContract }: { status: string; pageContract: AdminUiPageContract }) {
  return status === "active" ? (
    <Label variant="soft" color="success">{copy(pageContract, "market.status.active")}</Label>
  ) : (
    <Label variant="soft" color="default">{copy(pageContract, "market.status.retired")}</Label>
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
      <Stack direction="row" sx={{ alignItems: "center", gap: 1.5, minWidth: 0 }}>
        <Typography variant="body2" sx={{ flex: 1, minWidth: 0 }}>{city.name}</Typography>
        <StatusTag status={city.status} pageContract={pageContract} />
      </Stack>
    );
  }
  const flipped = city.status === "active" ? "retired" : "active";
  return (
    <MarketConfigForm action={updateMarketCityAction} outcomes={outcomes} data-market-city={city.id}>
      <input type="hidden" name="city_id" value={city.id} />
      <TextField
        size="small"
        name="name"
        defaultValue={city.name}
        required
        slotProps={{ htmlInput: { maxLength: 80, "aria-label": copy(pageContract, "market.field.city_name") } }}
        sx={{ flex: "1 1 180px" }}
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
      <Stack direction="row" sx={{ alignItems: "center", gap: 1.5, minWidth: 0 }}>
        <Typography variant="body2" sx={{ flex: 2, minWidth: 0 }}>{question.label}</Typography>
        <Typography variant="body2" sx={{ flex: 1, minWidth: 0, color: "text.secondary" }}>{question.unit_label}</Typography>
        <StatusTag status={question.status} pageContract={pageContract} />
      </Stack>
    );
  }
  const flipped = question.status === "active" ? "retired" : "active";
  return (
    <MarketConfigForm action={updateMarketQuestionAction} outcomes={outcomes} data-market-question={question.id}>
      <input type="hidden" name="question_id" value={question.id} />
      <TextField
        size="small"
        name="label"
        defaultValue={question.label}
        required
        slotProps={{ htmlInput: { maxLength: 80, "aria-label": copy(pageContract, "market.field.question_label") } }}
        sx={{ flex: "2 1 180px" }}
      />
      <TextField
        size="small"
        name="unit_label"
        defaultValue={question.unit_label}
        required
        slotProps={{ htmlInput: { maxLength: 24, "aria-label": copy(pageContract, "market.field.unit_label") } }}
        sx={{ flex: "1 1 96px" }}
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
