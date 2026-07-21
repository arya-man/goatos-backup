"use client";

import { useId, useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";

export function VaccineChipOverflow({
  vaccines,
  previewLimit,
  moreLabel,
  lessLabel,
  ariaLabel,
}: {
  vaccines: string[];
  previewLimit: number;
  moreLabel: string;
  lessLabel: string;
  ariaLabel: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const overflowId = useId();
  const hiddenCount = Math.max(0, vaccines.length - previewLimit);

  return (
    <div className="schedule-vaccine-control" title={vaccines.join(", ")}>
      <div className="schedule-chip-list vaccine-chip-list schedule-chip-list-compact" role="group" aria-label={ariaLabel}>
        {vaccines.slice(0, previewLimit).map((vaccine) => (
          <span key={vaccine} className="schedule-mini-chip vaccine-chip">{vaccine}</span>
        ))}
        <span id={overflowId} className="schedule-vaccine-expanded-chips" hidden={!expanded}>
          {vaccines.slice(previewLimit).map((vaccine) => (
            <span key={vaccine} className="schedule-mini-chip vaccine-chip">{vaccine}</span>
          ))}
        </span>
        <button
          type="button"
          className={`schedule-mini-chip schedule-more-chip schedule-vaccine-toggle${expanded ? " is-expanded" : ""}`}
          aria-expanded={expanded}
          aria-controls={overflowId}
          onClick={() => setExpanded((current) => !current)}
        >
          <span>{expanded ? lessLabel : `+${hiddenCount} ${moreLabel}`}</span>
          {expanded ? <ChevronUp className="ic" aria-hidden="true" /> : <ChevronDown className="ic" aria-hidden="true" />}
        </button>
      </div>
    </div>
  );
}
