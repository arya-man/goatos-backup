import { listOrEmpty } from "@/lib/list-or-empty";

import Link from "@/components/no-prefetch-link";
import Accordion from "@mui/material/Accordion";
import AccordionDetails from "@mui/material/AccordionDetails";
import AccordionSummary from "@mui/material/AccordionSummary";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Divider from "@mui/material/Divider";
import ListItemText from "@mui/material/ListItemText";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { salesErrorText } from "./sales-error";
import type { ApiResult } from "@/lib/api/server";
import type { MarketReporter } from "@/lib/api/market-server";
import { setMarketReporterAction } from "./market-actions";
import { MarketConfigForm, type MarketActionOutcomes } from "./market-config-form";
import Alert from "@mui/material/Alert";
import { EmptyState } from "@/components/app/empty-state";

/**
 * WHO makes the morning market calls (Sales SOP page, maintainer instruction 2026-09-19): the
 * people holding the market survey phone module, each with a one-tap give/take that writes
 * through the same person-access path /people uses. Everyone with a login is listed, reporters
 * first, so the survey's owner can hand the calls to someone new without leaving the page.
 */
export function MarketReportersSection({
  pageContract,
  result,
  canConfigure,
}: {
  pageContract: AdminUiPageContract;
  result: ApiResult<{ people: MarketReporter[] }>;
  canConfigure: boolean;
}) {
  const outcomes: MarketActionOutcomes = {
    market_reporter_saved: copy(pageContract, "action.market_reporter_saved"),
    market_save_failed: copy(pageContract, "action.market_save_failed"),
  };
  const people = result.ok ? listOrEmpty(result.data.people) : [];
  const reporters = people.filter((p) => p.reporter);
  const others = people.filter((p) => !p.reporter);
  const rowList = (list: MarketReporter[]) => (
    <Stack divider={<Divider flexItem sx={{ borderStyle: "dashed" }} />} spacing={1.5}>
      {list.map((p) => (
        <ReporterRow key={p.person_id} person={p} pageContract={pageContract} canConfigure={canConfigure} outcomes={outcomes} />
      ))}
    </Stack>
  );
  return (
    // Template account card: CardHeader (+ People link), current reporters list, the rest of the
    // team behind a template Accordion (was a native <details>, FJ3 P1-3).
    <Card component="section" aria-label={copy(pageContract, "market.reporters.aria")} data-testid="market-reporters">
      <CardHeader
        title={copy(pageContract, "market.reporters.title")}
        subheader={copy(pageContract, "market.reporters.sub")}
        action={
          <Button component={Link} href="/people" size="small" color="inherit" endIcon={<Iconify icon="eva:arrow-ios-forward-fill" />}>
            {copy(pageContract, "market.reporters.people_link")}
          </Button>
        }
      />
      <Stack spacing={2.5} sx={{ p: 3 }}>
        {!result.ok ? <Alert severity="error">{salesErrorText(result.error, copy(pageContract, "error.load"))}</Alert> : null}
        {!canConfigure ? <Alert severity="info">{copy(pageContract, "disabled.market_config")}</Alert> : null}
        <Stack direction="row" sx={{ alignItems: "center", gap: 1 }}>
          <Typography variant="subtitle1">{copy(pageContract, "market.reporters.current")}</Typography>
          <Label variant="soft" color={reporters.length ? "info" : "default"}>{reporters.length}</Label>
        </Stack>
        {reporters.length === 0 ? <EmptyState title={copy(pageContract, "market.reporters.none")} /> : rowList(reporters)}
        {canConfigure && others.length > 0 ? (
          <Accordion disableGutters variant="outlined" sx={{ "&::before": { display: "none" } }}>
            <AccordionSummary expandIcon={<Iconify icon="eva:arrow-ios-downward-fill" />}>
              <Typography variant="subtitle2">
                {copy(pageContract, "market.reporters.add")} ({others.length})
              </Typography>
            </AccordionSummary>
            <AccordionDetails>{rowList(others)}</AccordionDetails>
          </Accordion>
        ) : null}
      </Stack>
    </Card>
  );
}

function ReporterRow({
  person,
  pageContract,
  canConfigure,
  outcomes,
}: {
  person: MarketReporter;
  pageContract: AdminUiPageContract;
  canConfigure: boolean;
  outcomes: MarketActionOutcomes;
}) {
  const line = [person.title, person.park_label].filter(Boolean).join(" · ");
  if (!canConfigure) {
    return (
      <Stack direction="row" sx={{ alignItems: "center", gap: 1.5, minWidth: 0 }}>
        <ListItemText primary={person.display_name} secondary={line || undefined} sx={{ flex: 1, minWidth: 0, m: 0 }} />
        {person.reporter ? <Label variant="soft" color="success">{copy(pageContract, "market.reporters.tag")}</Label> : null}
      </Stack>
    );
  }
  return (
    <MarketConfigForm action={setMarketReporterAction} outcomes={outcomes} data-market-reporter={person.person_id}>
      <input type="hidden" name="person_id" value={person.person_id} />
      <input type="hidden" name="enabled" value={person.reporter ? "false" : "true"} />
      <ListItemText primary={person.display_name} secondary={line || undefined} sx={{ flex: "1 1 160px", minWidth: 0, m: 0 }} />
      {person.reporter ? <Label variant="soft" color="success">{copy(pageContract, "market.reporters.tag")}</Label> : null}
      <Button type="submit" variant="outlined" color={person.reporter ? "inherit" : "primary"}>
        {person.reporter ? copy(pageContract, "market.reporters.remove") : copy(pageContract, "market.reporters.give")}
      </Button>
    </MarketConfigForm>
  );
}
