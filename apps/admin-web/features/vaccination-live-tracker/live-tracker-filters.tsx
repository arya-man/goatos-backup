"use client";

import { useRouter } from "next/navigation";
import { useTransition } from "react";
import { Layers, Syringe, User } from "lucide-react";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

export type LiveFilterChoice = { value: string; label: string; href: string };

export type LiveFilterSpec = {
  id: string;
  label: string;
  allLabel: string;
  icon?: "layers" | "syringe" | "user";
  selected: string;
  choices: LiveFilterChoice[];
  clearHref: string;
};

// The persistent filter bar. Every option's destination href is computed on the SERVER through
// scopeHref, so this component never assembles a URL and never reads a scope key — it only navigates
// to an href it was handed. That is what keeps the top-bar park/date scope intact across a filter
// change instead of being silently dropped.
export function LiveTrackerFilters({
  filters,
  clearAllHref,
  optionsTruncated,
  pageContract,
}: {
  filters: LiveFilterSpec[];
  clearAllHref: string | null;
  optionsTruncated: boolean;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  function go(href: string) {
    startTransition(() => {
      router.push(href, { scroll: false });
    });
  }

  const active = filters.filter((filter) => filter.selected !== "");

  return (
    <div className={`lt-fbar${isPending ? " wfbusy" : ""}`} aria-busy={isPending}>
      {filters.map((filter) => {
        const Icon = filter.icon === "layers" ? Layers : filter.icon === "syringe" ? Syringe : filter.icon === "user" ? User : null;
        return (
          <span className="lt-fsel" key={filter.id}>
            {Icon ? <Icon className="ic" style={{ width: 13, height: 13 }} aria-hidden="true" /> : null}
            <select
              aria-label={filter.label}
              value={filter.selected}
              onChange={(event) => {
                const next = filter.choices.find((choice) => choice.value === event.target.value);
                go(next ? next.href : filter.clearHref);
              }}
            >
              <option value="">{filter.allLabel}</option>
              {filter.choices.map((choice) => (
                <option key={choice.value} value={choice.value}>
                  {choice.label}
                </option>
              ))}
            </select>
          </span>
        );
      })}

      <span className="lt-chips">
        {active.map((filter) => {
          const choice = filter.choices.find((candidate) => candidate.value === filter.selected);
          // NEVER fall through to the raw value. filter.selected is an internal identifier (a park or
          // shed uuid, a vaccine family token), and the vocabulary is compiled from the day's OWN
          // rows — so a selection that has no work on this drive day is simply absent from choices.
          // The old fallback then printed the uuid in the chip while the <select> beside it, having
          // no matching <option>, rendered "All parks": two controls contradicting each other while
          // the data really was narrowed.
          return (
            <span className="achip" key={filter.id}>
              {choice?.label ?? copy(pageContract, "filter.unlisted_selection")}
              <b
                role="button"
                tabIndex={0}
                aria-label={`${copy(pageContract, "filter.remove_one")} — ${filter.label}`}
                onClick={() => go(filter.clearHref)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") go(filter.clearHref);
                }}
              >
                ×
              </b>
            </span>
          );
        })}
        {clearAllHref ? (
          <button type="button" className="achip clr" onClick={() => go(clearAllHref)}>
            {copy(pageContract, "filter.clear_all")}
          </button>
        ) : null}
      </span>

      {optionsTruncated ? (
        <span className="lt-fnote">{copy(pageContract, "filter.truncated_note")}</span>
      ) : null}
      <span className="lt-fnote">{copy(pageContract, "filter.apply_note")}</span>
    </div>
  );
}
