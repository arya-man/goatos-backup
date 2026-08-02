"use client";

import { useRouter, useSearchParams } from "next/navigation";
import type { WeighingPlanner } from "./data";

export function ParkSelector({ planner }: { planner: WeighingPlanner }) {
  const router = useRouter();
  const searchParams = useSearchParams();

  const handleParkChange = (parkId: string) => {
    const params = new URLSearchParams(searchParams);
    params.set("park", parkId);
    router.push(`?${params.toString()}`);
  };

  return (
    <div className="weighing-builder-step">
      <div className="weighing-step-label">Step 2 · Park</div>
      <h3>Select one park</h3>
      {planner.parks.map((park) => (
        <label className={`weighing-choice${park.selected ? " on" : ""}`} key={park.id}>
          <input
            type="radio"
            name="park_id"
            value={park.id}
            defaultChecked={park.selected}
            onChange={(e) => handleParkChange(e.target.value)}
          />
          <span className="weighing-radio" />
          <div><b>{park.label}</b><small>{park.subtitle}</small></div>
          <strong>{park.kidCount}</strong>
        </label>
      ))}
    </div>
  );
}
