"use client";

// telemetry:exempt view-only chart filter over data the page already holds; no write, no data read

import { useState } from "react";
import Box from "@mui/material/Box";

import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { Label } from "@/components/minimal/label";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { SeriesLegend, SeriesLines, seriesColorVar, type LineSeries } from "@/components/svg-series";
import type { MarketSeries } from "@/lib/api/market-server";
import { istDayPlus } from "@/lib/format";

/** Backend copy the server page resolves and hands down; this file names no label of its own. */
export type MarketTrendLabels = {
  title: string;
  sub: string;
  aria: string;
  questionGroup: string;
  cityGroup: string;
  cityAll: string;
  empty: string;
};

/**
 * The market trend chart and its question / city chips (flicker fix, 2026-09-25).
 *
 * The chips used to be Links: every pick re-ran BOTH server reads for a series the page already
 * held, and the whole page waited on them. Picking a question or a city only chooses which of the
 * loaded series to draw, so it is CLIENT state now -- instant, no request -- and the URL follows
 * through replaceLocalOverlayUrl (no navigation) so a reload or a shared link keeps the pick.
 */
export function MarketTrendSection({
  series,
  questions,
  cities,
  from,
  today,
  initialQuestion,
  initialCity,
  labels,
}: {
  series: MarketSeries[];
  questions: { id: string; label: string }[];
  cities: { id: string; label: string }[];
  from: string;
  today: string;
  initialQuestion: string;
  initialCity: string;
  labels: MarketTrendLabels;
}) {
  const [question, setQuestion] = useState(initialQuestion);
  const [city, setCity] = useState(initialCity);

  const pick = (nextQuestion: string, nextCity: string) => {
    setQuestion(nextQuestion);
    setCity(nextCity);
    const url = new URL(window.location.href);
    if (nextQuestion && nextQuestion !== questions[0]?.id) url.searchParams.set("question", nextQuestion);
    else url.searchParams.delete("question");
    if (nextCity) url.searchParams.set("city", nextCity);
    else url.searchParams.delete("city");
    replaceLocalOverlayUrl(`${url.pathname}${url.search}`);
  };

  // Trend chart: the selected question, one line per city (and per unit within a city), on a
  // shared day axis that starts on the FIRST morning any price was recorded inside the window.
  // Days with no price are null so the line breaks honestly rather than interpolating.
  const trendSeries = series.filter((s) => s.question_id === question && (city === "" || s.city_id === city));
  const firstRecorded = series.reduce((min, s) => {
    const first = s.points[0]?.business_date ?? "";
    return first && (min === "" || first < min) ? first : min;
  }, "");
  const axisFrom = firstRecorded && firstRecorded > from ? firstRecorded : from;
  const dayKeys: string[] = [];
  for (let d = axisFrom; d <= today; d = istDayPlus(d, 1)) dayKeys.push(d);
  const dayIndex = new Map(dayKeys.map((d, i) => [d, i] as const));
  const lines: LineSeries[] = trendSeries.map((s, i) => {
    const points: (number | null)[] = dayKeys.map(() => null);
    for (const p of s.points) {
      const idx = dayIndex.get(p.business_date);
      if (idx !== undefined) points[idx] = p.price;
    }
    return { label: `${s.city_name} · ${s.unit_label}`, colorVar: seriesColorVar(i), points };
  });
  const questionLabel = questions.find((q) => q.id === question)?.label ?? "";
  const unit = trendSeries[0]?.unit_label ?? "";

  return (
    // Template chart card (AnalyticsWebsiteVisits anatomy): CardHeader (title, subheader, unit Label),
    // the question tabs and city pills as the card's filter row, legend, then the chart.
    <Card component="section" aria-label={labels.aria}>
      <CardHeader title={labels.title} subheader={labels.sub} action={unit ? <Label variant="soft" color="default">{unit}</Label> : null} />
      <Box sx={{ px: 3, pt: 2, pb: 3, minWidth: 0 }}>
      {/* Template tabs driven by client state (no href): a pick redraws from the series the page
          already holds, and the URL follows without a navigation. */}
      <Box sx={{ mb: 1.75 }}>
        <AnimatedTabs
          ariaLabel={labels.questionGroup}
          value={question}
          onChange={(next) => pick(next, city)}
          items={questions.map((q) => ({ value: q.id, label: q.label }))}
        />
      </Box>
      <Box sx={{ mt: 0.75, mb: 1.75 }}>
        <AnimatedTabs
          variant="pill"
          ariaLabel={labels.cityGroup}
          value={city}
          onChange={(next) => pick(question, next)}
          items={[{ value: "", label: labels.cityAll }, ...cities.map((c) => ({ value: c.id, label: c.label }))]}
        />
      </Box>
      <SeriesLegend entries={lines.map((l) => ({ label: l.label, colorVar: l.colorVar }))} />
      {/* Every survey morning is a category; the chart labels them DD/MM/YYYY at a regular step
          sized to the card, the last morning always labelled. */}
      <SeriesLines
        series={lines}
        dayLabels={dayKeys}
        valueNoun={unit || questionLabel}
        chartLabel={`${labels.title} · ${questionLabel}`}
        emptyLabel={labels.empty}
      />
      </Box>
    </Card>
  );
}
