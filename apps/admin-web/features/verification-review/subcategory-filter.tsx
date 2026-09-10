"use client";

import { useState } from "react";

type SubcategoryOption = {
  category: string;
  label: string;
};

export function SubcategoryFilter({
  ariaLabel,
  label,
  options,
  selectedCategories,
}: {
  ariaLabel: string;
  label: string;
  options: SubcategoryOption[];
  selectedCategories: string[];
}) {
  const [selected, setSelected] = useState(() => new Set(selectedCategories));

  function toggle(category: string) {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(category)) next.delete(category);
      else next.add(category);
      return next;
    });
  }

  return (
    <div className="vr-subfilter">
      <div className="vr-subfilter-head">
        <span>{label}</span>
      </div>
      <div className="vr-legend vr-sublegend" role="group" aria-label={ariaLabel}>
        {options.map((option) => {
          const checked = selected.has(option.category);
          return (
            <button
              key={option.category}
              type="button"
              className={`vr-lg vr-subchip${checked ? " on" : ""}`}
              aria-pressed={checked}
              onClick={() => toggle(option.category)}
            >
              {option.label}
            </button>
          );
        })}
      </div>
      {[...selected].map((category) => (
        <input key={category} type="hidden" name="category" value={category} />
      ))}
    </div>
  );
}
