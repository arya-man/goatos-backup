"use client";

// The Sex control in the Weights filter bar, beside Weighing.
//
// It is a plain select and it is deliberately built to look exactly like the URL-driven selects
// next to it — same `label` + muted caption + `tsize` select markup — because a reader should not
// have to know which of these controls costs a page load. What it does differently is invisible
// and is the point: it holds the choice in client state instead of the URL, because the answer for
// all three positions arrives in the SAME response (see gain-sex-scope for the measurements).
//
// It renders NO copy of its own: the field label and all three option labels arrive already
// resolved from the page contract.
import { useGainSex, type GainSex } from "./gain-sex-scope";

const ORDER: readonly GainSex[] = ["all", "male", "female"];

export function GainSexFilter({
  label,
  optionLabels,
}: {
  /** Field label from the page contract, e.g. "Sex". */
  label: string;
  /** Option labels by grain, already resolved from the page contract. */
  optionLabels: Record<GainSex, string>;
}) {
  const { sex, setSex } = useGainSex();
  return (
    <label style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}>
      <span className="muted">{label}</span>
      <select
        className="tsize"
        value={sex}
        aria-label={label}
        onChange={(event) => setSex(event.target.value as GainSex)}
      >
        {ORDER.map((option) => (
          <option key={option} value={option}>
            {optionLabels[option]}
          </option>
        ))}
      </select>
    </label>
  );
}
