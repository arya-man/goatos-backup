"use client";

/**
 * A duration, edited as a number and a unit.
 *
 * The requirement this exists to meet: any number of days, weeks, months or
 * years, not a fixed list of presets. Everything is STORED in days, because that
 * is what the scheduler works in; the unit is only how a person reads and types
 * it. Converting on the way in and out keeps "6 months" and "182 days" the same
 * value rather than two competing sources of truth.
 */

import { ChevronDown } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { formatDays, splitDays, type DurationUnit } from "./duration-format";

export { formatDays, splitDays } from "./duration-format";

const DAYS_PER = { days: 1, weeks: 7, months: 30, years: 365 } as const;
type Unit = DurationUnit;

type Props = {
  days: number;
  onChange: (days: number) => void;
  title: string;
  /** Shown when the field is not the plain variant (used inside sentences). */
  plain?: boolean;
  disabled?: boolean;
};

export function DurationField({ days, onChange, title, plain, disabled }: Props) {
  const [open, setOpen] = useState(false);
  const initial = splitDays(days);
  const [value, setValue] = useState(String(initial.value));
  const [unit, setUnit] = useState<Unit>(initial.unit);
  const box = useRef<HTMLSpanElement>(null);
  // The last value this field itself committed. While the user is typing, `days`
  // comes back changed on every keystroke, and re-deriving the unit from it
  // rewrote the number under their fingers: typing "3010" days flipped to
  // "1 month" at the third keystroke, and the remaining digits were then read as
  // MONTHS -- 3300 days saved for 3010 typed. Only a change this field did not
  // make is allowed to re-sync it.
  const committed = useRef<number | null>(null);
  // Set when what is typed cannot be a duration, so the field can say so rather than
  // look as though it took effect.
  const [invalid, setInvalid] = useState(false);

  useEffect(() => {
    if (committed.current === days) return;
    const next = splitDays(days);
    setValue(String(next.value));
    setUnit(next.unit);
  }, [days]);

  useEffect(() => {
    if (!open) return;
    function onDocClick(event: MouseEvent) {
      if (box.current && !box.current.contains(event.target as Node)) setOpen(false);
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  function commit(nextValue: string, nextUnit: Unit): boolean {
    const n = Number(nextValue);
    // Zero and negatives are not durations. Refusing them here means the
    // scheduler never receives one.
    //
    // Nor are fractions. Rounding one silently was the same bug this field was
    // rebuilt to kill, just in a different disguise: "1.5 weeks" committed 14 days
    // while the open input still read 1.5, so the farm saw one interval and the
    // scheduler used another. A fraction is refused and said so instead.
    if (!Number.isFinite(n) || n <= 0 || !Number.isInteger(n)) return false;
    const total = n * DAYS_PER[nextUnit];
    committed.current = total;
    onChange(total);
    return true;
  }

  return (
    <span className="vp-dur" ref={box}>
      <button
        className={plain ? "f plain" : "f"}
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        aria-haspopup="dialog"
        disabled={disabled}
      >
        {formatDays(days)} <ChevronDown className="car" size={11} aria-hidden />
      </button>
      {open ? (
        <span className="pop pop-num" role="dialog" aria-label={title}>
          <span className="pn-l">{title}</span>
          <span className="durrow">
            <input
              className="num"
              type="number"
              min={1}
              value={value}
              autoFocus
              onChange={(e) => {
                setValue(e.target.value);
                setInvalid(!commit(e.target.value, unit) && e.target.value.trim() !== "");
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") setOpen(false);
              }}
              aria-label="How many"
            />
            <select
              className="durunit"
              value={unit}
              onChange={(e) => {
                const next = e.target.value as Unit;
                setUnit(next);
                // A unit change with an unusable number used to be dropped in silence:
                // the select moved, nothing was saved, and the chip still showed the old
                // unit. Now the field says why.
                setInvalid(!commit(value, next));
              }}
              aria-label="Unit"
            >
              <option value="days">days</option>
              <option value="weeks">weeks</option>
              <option value="months">months</option>
              <option value="years">years</option>
            </select>
          </span>
          {invalid ? (
            <span className="durwhy" role="status">
              Whole numbers only, and at least 1.
            </span>
          ) : null}
        </span>
      ) : null}
    </span>
  );
}
