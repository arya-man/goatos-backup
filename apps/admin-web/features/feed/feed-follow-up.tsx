"use client";

import { useMemo, useState } from "react";

import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedAnalyticsFollowUpResponse, FeedAnalyticsFollowUpRow } from "@/lib/api/server";

// ---------------------------------------------------------------------------
// Feed follow-up: did the sheet react when animals were bought, sold or lost?
//
// Every number and every word here is the backend's (AGENTS.md golden frontend
// rule). This file owns the LOOK of the answer -- which bar, which colour, what
// is open -- and computes no business figure of its own. In particular it does
// NOT re-derive a verdict: `status` arrives per pen and per day, and a client
// that recomputed it would be a second opinion able to disagree with the
// server's on the same row.
//
// THREE THINGS IN ORDER, because that is the order the question is asked in:
//   1. the verdict strip -- how many pens need looking at
//   2. the walk -- what happened to the herd over the range
//   3. the table -- which pen, and (expanded) which day and which animals
// ---------------------------------------------------------------------------

type Row = FeedAnalyticsFollowUpRow;
type StatusFilter = "all" | "not_followed" | "pending" | "followed";

const num = (raw: string) => {
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : 0;
};
const kg = (raw: string) => (raw === "" ? "—" : Number(num(raw).toFixed(1)).toLocaleString("en-IN"));

/** One key per pen. Shed id plus partition, never the pen NAME: names repeat across parks. */
const penKey = (row: Row) => `${row.park_id}:${row.shed_id}:${row.partition_label}`;

function fa(pageContract: AdminUiPageContract, key: string): string {
  return copy(pageContract, key);
}

export function FeedFollowUpTab({
  data,
  pageContract,
}: {
  data: FeedAnalyticsFollowUpResponse;
  pageContract: AdminUiPageContract;
}) {
  const [filter, setFilter] = useState<StatusFilter>("all");
  const [open, setOpen] = useState<string | null>(null);

  const rows = useMemo(
    () => (filter === "all" ? data.rows : data.rows.filter((row) => row.status === filter)),
    [data.rows, filter],
  );

  if (data.rows.length === 0) {
    return (
      <section className="card">
        <h2 className="h">{fa(pageContract, "followup.table.title")}</h2>
        <p className="muted small">{fa(pageContract, "followup.empty")}</p>
      </section>
    );
  }

  return (
    <>
      <p className="muted small" style={{ margin: "4px 0 0" }}>{fa(pageContract, "followup.hint")}</p>

      {/* 1. THE VERDICT STRIP. "Not followed" leads and is the only tile with a
          warning tone, because it is the only one anyone has to act on. Each
          tile is a filter over the served rows -- local view state, no refetch. */}
      <div className="ffu-strip" role="group" aria-label={fa(pageContract, "followup.filter.aria")}>
        <StatusTile
          tone="dng"
          active={filter === "not_followed"}
          count={data.totals.not_followed}
          label={fa(pageContract, "followup.status.not_followed")}
          sub={fa(pageContract, "followup.status.not_followed.sub")}
          onClick={() => setFilter(filter === "not_followed" ? "all" : "not_followed")}
        />
        <StatusTile
          tone="warn"
          active={filter === "pending"}
          count={data.totals.pending}
          label={fa(pageContract, "followup.status.pending")}
          sub={fa(pageContract, "followup.status.pending.sub")}
          onClick={() => setFilter(filter === "pending" ? "all" : "pending")}
        />
        <StatusTile
          tone="ok"
          active={filter === "followed"}
          count={data.totals.followed}
          label={fa(pageContract, "followup.status.followed")}
          sub={fa(pageContract, "followup.status.followed.sub")}
          onClick={() => setFilter(filter === "followed" ? "all" : "followed")}
        />
      </div>

      {/* 2. THE WALK. */}
      <section className="card">
        <h2 className="h">{fa(pageContract, "followup.walk.title")}</h2>
        <Waterfall totals={data.totals} pageContract={pageContract} />
        <p className="muted small" style={{ margin: "10px 0 0" }}>{fa(pageContract, "followup.walk.note")}</p>
      </section>

      {/* 3. THE TABLE. */}
      <section className="card">
        <h2 className="h">{fa(pageContract, "followup.table.title")}</h2>
        <div className="tablewrap" tabIndex={0} role="group" aria-label={fa(pageContract, "followup.table.title")}>
          <table className="tbl ffu-table">
            <thead>
              <tr>
                <th>{fa(pageContract, "followup.col.pen")}</th>
                <th>{fa(pageContract, "followup.col.changes")}</th>
                <th className="r">{fa(pageContract, "followup.col.animals")}</th>
                <th className="r">{fa(pageContract, "followup.col.feed")}</th>
                <th>{fa(pageContract, "followup.col.status")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={5} className="muted small">{fa(pageContract, "followup.empty.filtered")}</td>
                </tr>
              ) : null}
              {rows.map((row) => {
                const key = penKey(row);
                const isOpen = open === key;
                return (
                  <PenRows
                    key={key}
                    row={row}
                    open={isOpen}
                    onToggle={() => setOpen(isOpen ? null : key)}
                    pageContract={pageContract}
                  />
                );
              })}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}

function StatusTile({
  tone,
  count,
  label,
  sub,
  active,
  onClick,
}: {
  tone: "ok" | "warn" | "dng";
  count: number;
  label: string;
  sub: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={`ffu-tile ffu-${tone}${active ? " on" : ""}`}
      aria-pressed={active}
      onClick={onClick}
    >
      <span className="ffu-tile-n">{count.toLocaleString("en-IN")}</span>
      <span className="ffu-tile-l">{label}</span>
      <span className="ffu-tile-s muted small">{sub}</span>
    </button>
  );
}

// ---------------------------------------------------------------------------
// The walk, drawn as a waterfall: one number pushed up and down by named
// causes, which is exactly what a waterfall is for.
//
// It is drawn from the SERVED totals and closes only if they close. When they
// do not -- animals shifted between pens, which this tab does not count as a
// cause -- the remainder is drawn as its own bar rather than quietly folded
// into the end bar, so the chart never claims an arithmetic it does not have.
// ---------------------------------------------------------------------------

type WalkBar = { label: string; value: number; kind: "end" | "up" | "down" | "gap" };

function Waterfall({
  totals,
  pageContract,
}: {
  totals: FeedAnalyticsFollowUpResponse["totals"];
  pageContract: AdminUiPageContract;
}) {
  const bars: WalkBar[] = [
    { label: fa(pageContract, "followup.walk.start"), value: totals.start_animals, kind: "end" },
    { label: fa(pageContract, "followup.walk.purchased"), value: totals.purchased, kind: "up" },
    { label: fa(pageContract, "followup.walk.sold"), value: -totals.sold, kind: "down" },
    { label: fa(pageContract, "followup.walk.died"), value: -totals.died, kind: "down" },
  ];
  // A zero remainder is the ordinary case and adds nothing to read, so the bar
  // appears only when there is something to explain.
  if (totals.unexplained !== 0) {
    bars.push({ label: fa(pageContract, "followup.walk.unexplained"), value: totals.unexplained, kind: "gap" });
  }
  bars.push({ label: fa(pageContract, "followup.walk.end"), value: totals.end_animals, kind: "end" });

  // Running tops, so each middle bar floats where it actually acts.
  let running = 0;
  const spans = bars.map((bar) => {
    if (bar.kind === "end") {
      running = bar.value;
      return { bar, from: 0, to: bar.value };
    }
    const from = running;
    running += bar.value;
    return { bar, from, to: running };
  });
  const ceiling = Math.max(1, ...spans.map((s) => Math.max(s.from, s.to)));

  const W = 720;
  const H = 210;
  const padB = 46;
  const padT = 14;
  const slot = W / spans.length;
  const barW = Math.min(64, slot * 0.54);
  const y = (v: number) => padT + (1 - v / ceiling) * (H - padT - padB);

  // The THEME's own tokens. `dng`/`mut` are Tag class suffixes, not colour
  // variables -- using them here painted the Sold, Died and Unexplained bars
  // black on the first render, because an undefined custom property falls back
  // to the SVG default fill.
  const fill: Record<WalkBar["kind"], string> = {
    end: "var(--brand)",
    up: "var(--ok)",
    down: "var(--danger)",
    gap: "var(--muted)",
  };

  return (
    <svg
      className="ffu-walk"
      viewBox={`0 0 ${W} ${H}`}
      role="img"
      aria-label={fa(pageContract, "followup.walk.title")}
    >
      {spans.map(({ bar, from, to }, i) => {
        const cx = i * slot + slot / 2;
        const top = Math.min(y(from), y(to));
        const height = Math.max(2, Math.abs(y(from) - y(to)));
        const sign = bar.kind === "up" || bar.kind === "gap" ? (bar.value > 0 ? "+" : "") : bar.kind === "down" ? "" : "";
        return (
          <g key={bar.label}>
            {/* The connector to the next bar: the eye follows the level across,
                which is what makes a waterfall readable as one running number. */}
            {i < spans.length - 1 ? (
              <line
                x1={cx + barW / 2}
                x2={(i + 1) * slot + slot / 2 - barW / 2}
                y1={y(to)}
                y2={y(to)}
                stroke="var(--line)"
                strokeDasharray="3 3"
              />
            ) : null}
            <rect x={cx - barW / 2} y={top} width={barW} height={height} rx={3} fill={fill[bar.kind]} />
            <text x={cx} y={top - 5} textAnchor="middle" className="ffu-walk-v">
              {bar.kind === "end" ? bar.value.toLocaleString("en-IN") : `${sign}${bar.value.toLocaleString("en-IN")}`}
            </text>
            <text x={cx} y={H - padB + 20} textAnchor="middle" className="ffu-walk-l">
              {bar.label}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

// ---------------------------------------------------------------------------
// One pen: the summary row, and (expanded) one block per day something
// happened, each naming the two sheets it was judged on.
// ---------------------------------------------------------------------------

function PenRows({
  row,
  open,
  onToggle,
  pageContract,
}: {
  row: Row;
  open: boolean;
  onToggle: () => void;
  pageContract: AdminUiPageContract;
}) {
  const statusTone = row.status === "not_followed" ? "dng" : row.status === "pending" ? "warn" : "ok";
  const statusLabel = fa(pageContract, `followup.status.${row.status}`);
  return (
    <>
      <tr className={`ffu-row${open ? " on" : ""}`} onClick={onToggle}>
        <td>
          <button
            type="button"
            className="ffu-expand"
            aria-expanded={open}
            aria-label={fa(pageContract, open ? "followup.collapse" : "followup.expand")}
            onClick={(event) => {
              event.stopPropagation();
              onToggle();
            }}
          >
            {open ? "▾" : "▸"}
          </button>
          {/* Backend-composed pen name, rendered verbatim (operational-location convention). */}
          <strong>{row.operational_location_display}</strong>
          <span className="muted small"> · {row.park_label}</span>
        </td>
        <td>
          <CauseChips row={row} pageContract={pageContract} />
        </td>
        <td className="r nums">
          <Movement before={row.head_before.toLocaleString("en-IN")} after={row.head_after.toLocaleString("en-IN")} />
        </td>
        <td className="r nums">
          <Movement before={kg(row.kg_before)} after={kg(row.kg_after)} />
        </td>
        <td>
          <Tag tone={statusTone}>{statusLabel}</Tag>
        </td>
      </tr>
      {open ? (
        <tr className="ffu-detail">
          <td colSpan={5}>
            <div className="ffu-days">
              {row.days.map((day) => (
                <DayBlock key={day.event_date} day={day} pageContract={pageContract} />
              ))}
            </div>
          </td>
        </tr>
      ) : null}
    </>
  );
}

function CauseChips({ row, pageContract }: { row: Row; pageContract: AdminUiPageContract }) {
  const chips: { key: string; tone: "ok" | "info" | "dng"; text: string }[] = [];
  if (row.purchased > 0) {
    chips.push({ key: "purchased", tone: "ok", text: `${fa(pageContract, "followup.cause.purchased")} +${row.purchased}` });
  }
  if (row.sold > 0) {
    chips.push({ key: "sold", tone: "info", text: `${fa(pageContract, "followup.cause.sold")} −${row.sold}` });
  }
  if (row.died > 0) {
    chips.push({ key: "died", tone: "dng", text: `${fa(pageContract, "followup.cause.died")} −${row.died}` });
  }
  return (
    <span className="ffu-chips">
      {chips.map((chip) => (
        <Tag key={chip.key} tone={chip.tone}>
          {chip.text}
        </Tag>
      ))}
    </span>
  );
}

/** "44 → 41", the whole point of the column, with the arrow carrying the direction. */
function Movement({ before, after }: { before: string; after: string }) {
  const changed = before !== after;
  return (
    <span className={`ffu-move${changed ? " changed" : ""}`}>
      <span className="ffu-from">{before}</span>
      <span className="ffu-arrow" aria-hidden="true">→</span>
      <span className="ffu-to">{after}</span>
    </span>
  );
}

function DayBlock({
  day,
  pageContract,
}: {
  day: Row["days"][number];
  pageContract: AdminUiPageContract;
}) {
  const tone = day.status === "not_followed" ? "dng" : day.status === "pending" ? "warn" : "ok";
  return (
    <div className={`ffu-day ffu-${tone}`}>
      <div className="ffu-day-head">
        <strong>{day.event_date}</strong>
        <Tag tone={tone}>{fa(pageContract, `followup.status.${day.status}`)}</Tag>
        {/* The head count moved and the quantity did not. The verdict stays on
            the head count -- that is the number the sheet is computed from, and
            an experiment pen's kg is authored flat on purpose -- but a reader
            asking "was the feed reduced" is owed this plainly rather than
            having to spot it in the two columns. */}
        {day.head_delta !== 0 && day.kg_before !== "" && day.kg_before === day.kg_after ? (
          <Tag tone="warn" title={fa(pageContract, "followup.feedflat.help")}>
            {fa(pageContract, "followup.feedflat.chip")}
          </Tag>
        ) : null}
        {day.unexplained !== 0 ? (
          <Tag tone="mut" title={fa(pageContract, "followup.unexplained.help")}>
            {fa(pageContract, "followup.unexplained.chip").replace(
              "{count}",
              (day.unexplained > 0 ? `+${day.unexplained}` : String(day.unexplained)),
            )}
          </Tag>
        ) : null}
      </div>
      <ul className="ffu-events">
        {day.events.map((event) => (
          <li key={`${event.kind}-${event.event_date}`}>
            <Tag tone={event.kind === "purchased" ? "ok" : event.kind === "sold" ? "info" : "dng"}>
              {fa(pageContract, `followup.cause.${event.kind}`)} {event.animals}
            </Tag>{" "}
            {/* Identifiers, not goat ids: the reader can walk to the pen and check the animal. */}
            <span className="ffu-tags">{event.tags.join(", ")}</span>
            {event.tags_total > event.tags.length ? (
              <span className="muted small">
                {" "}
                {fa(pageContract, "followup.tags.more").replace(
                  "{count}",
                  String(event.tags_total - event.tags.length),
                )}
              </span>
            ) : null}
          </li>
        ))}
      </ul>
      {/* The two sheets the verdict was read off. Naming them is what keeps a
          negative verdict an observation rather than an accusation. */}
      {day.after_day === "" ? (
        <p className="muted small ffu-sheets">{fa(pageContract, "followup.day.pending")}</p>
      ) : (
        <p className="muted small ffu-sheets">
          {fa(pageContract, "followup.day.before").replace("{date}", day.before_day)}
          {" · "}
          {fa(pageContract, "followup.day.after").replace("{date}", day.after_day)}
          {" · "}
          {fa(pageContract, "followup.day.animals")
            .replace("{before}", String(day.head_before))
            .replace("{after}", String(day.head_after))}
          {" · "}
          {fa(pageContract, "followup.day.feed")
            .replace("{before}", kg(day.kg_before))
            .replace("{after}", kg(day.kg_after))}
        </p>
      )}
    </div>
  );
}
