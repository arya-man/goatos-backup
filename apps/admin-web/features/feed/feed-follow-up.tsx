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
        <WalkSummary totals={data.totals} pageContract={pageContract} />
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
// The walk, read as a sentence rather than drawn as a chart.
//
// It WAS a waterfall (maintainer request 2026-09-22 replaced it). A waterfall
// puts each cause on a floating bar whose height is its size and whose position
// is the running total, which means the reader has to decode two things at once
// and the small causes -- a two-animal death against a 344 start -- come out as
// a sliver nobody can read. There are six numbers here and one question: what
// came in, what went out, where did it start and end. Six plain figures answer
// it faster than any geometry.
//
// Still drawn from the SERVED totals, and still only closing if they close:
// when animals moved between pens, the remainder stands as its own figure
// rather than being folded into the end.
// ---------------------------------------------------------------------------

function WalkSummary({
  totals,
  pageContract,
}: {
  totals: FeedAnalyticsFollowUpResponse["totals"];
  pageContract: AdminUiPageContract;
}) {
  const n = (value: number) => value.toLocaleString("en-IN");
  const signed = (value: number) => (value > 0 ? `+${n(value)}` : n(value));
  const causes: { key: string; label: string; value: string; tone: string }[] = [
    { key: "bought", label: fa(pageContract, "followup.walk.purchased"), value: signed(totals.purchased), tone: "ok" },
    { key: "sold", label: fa(pageContract, "followup.walk.sold"), value: signed(-totals.sold), tone: "dng" },
    { key: "died", label: fa(pageContract, "followup.walk.died"), value: signed(-totals.died), tone: "dng" },
  ];
  // A zero remainder is the ordinary case and adds nothing to read, so the
  // figure appears only when there is something to explain.
  if (totals.unexplained !== 0) {
    causes.push({
      key: "unexplained",
      label: fa(pageContract, "followup.walk.unexplained"),
      value: signed(totals.unexplained),
      tone: "mut",
    });
  }
  return (
    <div className="ffu-walk">
      <div className="ffu-walk-end">
        <span className="ffu-walk-n">{n(totals.start_animals)}</span>
        <span className="ffu-walk-l">{fa(pageContract, "followup.walk.start")}</span>
      </div>
      <div className="ffu-walk-causes">
        {causes.map((cause) => (
          <div className={`ffu-walk-cause ffu-${cause.tone}`} key={cause.key}>
            <span className="ffu-walk-cn">{cause.value}</span>
            <span className="ffu-walk-l">{cause.label}</span>
          </div>
        ))}
      </div>
      <div className="ffu-walk-end">
        <span className="ffu-walk-n">{n(totals.end_animals)}</span>
        <span className="ffu-walk-l">{fa(pageContract, "followup.walk.end")}</span>
      </div>
    </div>
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
        {/* A pen still waiting has no "after" reading, so both columns say so
            rather than printing the zero value as if the sheet fed nobody. */}
        <td className="r nums">
          <Movement
            before={row.head_before.toLocaleString("en-IN")}
            after={row.last_day === "" ? "—" : row.head_after.toLocaleString("en-IN")}
          />
        </td>
        <td className="r nums">
          <Movement before={kg(row.kg_before)} after={row.last_day === "" ? "—" : kg(row.kg_after)} />
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
                <DayBlock key={day.expected_day} day={day} pageContract={pageContract} />
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
        {/* The day the feed was SUPPOSED to change -- not the day the animal
            moved. After the afternoon cut-off those are two different days, and
            naming the wrong one makes the verdict look wrong. */}
        <strong>{fa(pageContract, "followup.day.title").replace("{date}", day.expected_day)}</strong>
        <Tag tone={tone}>{fa(pageContract, `followup.status.${day.status}`)}</Tag>
        {day.head_delta !== 0 && day.kg_before !== "" && day.kg_before === day.kg_after ? (
          <Tag tone="warn" title={fa(pageContract, "followup.feedflat.help")}>
            {fa(pageContract, "followup.feedflat.chip")}
          </Tag>
        ) : null}
        {day.unexplained !== 0 ? (
          <Tag tone="mut" title={fa(pageContract, "followup.unexplained.help")}>
            {fa(pageContract, "followup.unexplained.chip").replace(
              "{count}",
              day.unexplained > 0 ? `+${day.unexplained}` : String(day.unexplained),
            )}
          </Tag>
        ) : null}
      </div>
      <ul className="ffu-events">
        {day.events.map((event) => (
          <li key={`${event.kind}-${event.event_date}-${String(event.after_cutoff)}`}>
            <Tag tone={event.kind === "purchased" ? "ok" : event.kind === "sold" ? "info" : "dng"}>
              {fa(pageContract, `followup.cause.${event.kind}`)} {event.animals}
            </Tag>{" "}
            <span className="muted small">{event.event_date}</span>{" "}
            {/* Why THIS check and not the next day's: the cut-off the park
                actually runs on, which the backend resolved per park and date. */}
            <span className="muted small">
              {fa(pageContract, event.after_cutoff ? "followup.day.late" : "followup.day.early")
                .replace("{time}", day.cutoff_time)}
            </span>
            <div className="ffu-tags">
              {event.tags.join(", ")}
              {event.tags_total > event.tags.length ? (
                <span className="muted small">
                  {" "}
                  {fa(pageContract, "followup.tags.more").replace(
                    "{count}",
                    String(event.tags_total - event.tags.length),
                  )}
                </span>
              ) : null}
            </div>
          </li>
        ))}
      </ul>
      {/* The two sheets the verdict was read off. Naming them is what keeps a
          negative verdict an observation rather than an accusation. */}
      {day.after_day === "" ? (
        <p className="muted small ffu-sheets">{fa(pageContract, "followup.day.pending")}</p>
      ) : (
        <p className="muted small ffu-sheets">
          {fa(pageContract, "followup.day.sheets")
            .replace("{before}", day.before_day)
            .replace("{after}", day.after_day)}
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
