"use client";

import { useMemo, useState } from "react";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { operationalLocationLabel } from "@/lib/operational-location";

export type ProcurementLocationOption = {
  id: string;
  code: string | null;
  name: string;
  parentId: string | null;
  // Optional operational-location metadata: present once the backend contract for this
  // picker emits it (see contracts/openapi/app-api.yaml -> ShiftingDestinationShed for the
  // precedent shape). Absent means treat this option as non-partitioned.
  partitionLabel?: string | null;
  sourceShedName?: string | null;
  operationalLocationDisplay?: string | null;
  animalCount?: number | null;
};

export type ProcurementLocations = {
  parks: ProcurementLocationOption[];
  origins: ProcurementLocationOption[];
  sheds: ProcurementLocationOption[];
  available: boolean;
};

function locationLabel(location: ProcurementLocationOption): string {
  const baseName =
    location.operationalLocationDisplay ||
    operationalLocationLabel({
      shedName: location.name,
      partitionLabel: location.partitionLabel,
      sourceShedName: location.sourceShedName,
    });
  const withCount = typeof location.animalCount === "number" ? `${baseName} (${location.animalCount})` : baseName;
  return location.code ? `${location.code} - ${withCount}` : withCount;
}

export function ParkLocationSelect({
  name,
  parks,
  pageContract,
  required = true,
}: {
  name: string;
  parks: ProcurementLocationOption[];
  pageContract: AdminUiPageContract;
  required?: boolean;
}) {
  const disabled = parks.length === 0;
  return (
    <select
      name={name}
      required={required}
      defaultValue=""
      disabled={disabled}
      title={disabled ? copy(pageContract, "location.no_parks") : undefined}
    >
      <option value="">
        {disabled ? copy(pageContract, "location.no_parks") : copy(pageContract, "location.select_park")}
      </option>
      {parks.map((park) => (
        <option key={park.id} value={park.id}>
          {locationLabel(park)}
        </option>
      ))}
    </select>
  );
}

export function OptionalLocationSelect({
  name,
  locations,
  pageContract,
  defaultValue = "",
}: {
  name: string;
  locations: ProcurementLocationOption[];
  pageContract: AdminUiPageContract;
  defaultValue?: string;
}) {
  const disabled = locations.length === 0;
  return (
    <select name={name} defaultValue={defaultValue} disabled={disabled} title={disabled ? copy(pageContract, "location.no_origins") : undefined}>
      <option value="">
        {disabled ? copy(pageContract, "location.no_origins") : copy(pageContract, "location.select_optional_location")}
      </option>
      {locations.map((location) => (
        <option key={location.id} value={location.id}>
          {locationLabel(location)}
        </option>
      ))}
    </select>
  );
}

export function ParkShedLocationSelects({
  parks,
  sheds,
  pageContract,
}: {
  parks: ProcurementLocationOption[];
  sheds: ProcurementLocationOption[];
  pageContract: AdminUiPageContract;
}) {
  const [parkId, setParkId] = useState("");
  const [shedId, setShedId] = useState("");
  const parkSheds = useMemo(() => (parkId ? sheds.filter((shed) => shed.parentId === parkId) : []), [parkId, sheds]);
  const parkDisabled = parks.length === 0;
  const shedDisabled = !parkId || parkSheds.length === 0;
  const shedTitle = !parkId
    ? copy(pageContract, "location.select_park_first")
    : parkSheds.length === 0
      ? copy(pageContract, "location.no_sheds_for_park")
      : undefined;

  return (
    <>
      <div className="fld" style={{ flex: 1, minWidth: 180 }}>
        <label>{copy(pageContract, "field.park_location_id")}</label>
        <select
          name="park_location_id"
          required
          value={parkId}
          onChange={(event) => {
            setParkId(event.target.value);
            setShedId("");
          }}
          disabled={parkDisabled}
          title={parkDisabled ? copy(pageContract, "location.no_parks") : undefined}
        >
          <option value="">
            {parkDisabled ? copy(pageContract, "location.no_parks") : copy(pageContract, "location.select_park")}
          </option>
          {parks.map((park) => (
            <option key={park.id} value={park.id}>
              {locationLabel(park)}
            </option>
          ))}
        </select>
      </div>
      <div className="fld" style={{ flex: 1, minWidth: 180 }}>
        <label>{copy(pageContract, "field.shed_location_id")}</label>
        <select name="shed_location_id" required value={shedId} disabled={shedDisabled} onChange={(event) => setShedId(event.target.value)} title={shedTitle}>
          <option value="">
            {!parkId
              ? copy(pageContract, "location.select_park_first")
              : parkSheds.length === 0
                ? copy(pageContract, "location.no_sheds_for_park")
                : copy(pageContract, "location.select_shed")}
          </option>
          {parkSheds.map((shed) => (
            <option key={shed.id} value={shed.id}>
              {locationLabel(shed)}
            </option>
          ))}
        </select>
      </div>
    </>
  );
}
