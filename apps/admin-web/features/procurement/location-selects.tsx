"use client";

import { useMemo, useState } from "react";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { operationalLocationLabel } from "@/lib/operational-location";
import Grid from "@mui/material/Grid";
import { FormSelect } from "./form-select";
import { listOptions } from "./option-utils";
import { HiddenField } from "@/components/app/hidden-field";

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

function optionKey(location: ProcurementLocationOption): string {
  return `${location.id}|${location.partitionLabel ?? ""}`;
}

export function ParkLocationSelect({
  name,
  label,
  parks,
  pageContract,
  required = true,
}: {
  name: string;
  label: string;
  parks: ProcurementLocationOption[];
  pageContract: AdminUiPageContract;
  required?: boolean;
}) {
  const disabled = parks.length === 0;
  return (
    <FormSelect
      label={label}
      name={name}
      size="small"
      fullWidth
      required={required}
      defaultValue=""
      disabled={disabled}
      title={disabled ? copy(pageContract, "location.no_parks") : undefined}
      options={listOptions(
        parks,
        (park) => park.id,
        (park) => locationLabel(park),
        disabled ? copy(pageContract, "location.no_parks") : copy(pageContract, "location.select_park"),
      )}
    />
  );
}

export function OptionalLocationSelect({
  name,
  label,
  locations,
  pageContract,
  defaultValue = "",
}: {
  name: string;
  label: string;
  locations: ProcurementLocationOption[];
  pageContract: AdminUiPageContract;
  defaultValue?: string;
}) {
  const disabled = locations.length === 0;
  return (
    <FormSelect
      label={label}
      name={name}
      size="small"
      fullWidth
      defaultValue={defaultValue}
      disabled={disabled}
      title={disabled ? copy(pageContract, "location.no_origins") : undefined}
      options={listOptions(
        locations,
        (location) => location.id,
        (location) => locationLabel(location),
        disabled ? copy(pageContract, "location.no_origins") : copy(pageContract, "location.select_optional_location"),
      )}
    />
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
  const [shedKey, setShedKey] = useState("");
  const parkSheds = useMemo(() => (parkId ? sheds.filter((shed) => shed.parentId === parkId) : []), [parkId, sheds]);
  const selectedShed = parkSheds.find((shed) => optionKey(shed) === shedKey) ?? null;
  const parkDisabled = parks.length === 0;
  const shedDisabled = !parkId || parkSheds.length === 0;
  const shedTitle = !parkId
    ? copy(pageContract, "location.select_park_first")
    : parkSheds.length === 0
      ? copy(pageContract, "location.no_sheds_for_park")
      : undefined;

  return (
    <>
      <Grid size={{ xs: 12, sm: 6 }}>
        <FormSelect
          size="small"
          fullWidth
          label={copy(pageContract, "field.park_location_id")}
          name="park_location_id"
          required
          value={parkId}
          onValueChange={(next) => {
            setParkId(next);
            setShedKey("");
          }}
          disabled={parkDisabled}
          title={parkDisabled ? copy(pageContract, "location.no_parks") : undefined}
          options={listOptions(
            parks,
            (park) => park.id,
            (park) => locationLabel(park),
            parkDisabled ? copy(pageContract, "location.no_parks") : copy(pageContract, "location.select_park"),
          )}
        />
      </Grid>
      <Grid size={{ xs: 12, sm: 6 }}>
        <HiddenField name="shed_location_id" value={selectedShed?.id ?? ""} />
        <HiddenField name="partition_label" value={selectedShed?.partitionLabel ?? ""} />
        <FormSelect
          size="small"
          fullWidth
          label={copy(pageContract, "field.shed_location_id")}
          required
          value={shedKey}
          disabled={shedDisabled}
          onValueChange={setShedKey}
          title={shedTitle}
          options={listOptions(
            parkSheds,
            (shed) => optionKey(shed),
            (shed) => locationLabel(shed),
            !parkId
              ? copy(pageContract, "location.select_park_first")
              : parkSheds.length === 0
                ? copy(pageContract, "location.no_sheds_for_park")
                : copy(pageContract, "location.select_shed"),
          )}
        />
      </Grid>
    </>
  );
}
