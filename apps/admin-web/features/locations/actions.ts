"use server";

import { randomUUID } from "node:crypto";
import {
  actionErrorMessage,
  actionRedirect,
  optionalBoolean,
  optionalString,
  requiredNumber,
  requiredString,
} from "@/lib/action-helpers";
import {
  createLocation,
  createLocationAlias,
  createLocationCapacity,
  deleteLocation,
  deleteLocationAlias,
  deleteLocationCapacity,
  resolveLocationReviewItem,
  retireLocation,
  retireLocationAlias,
  updateLocation,
  updateLocationAlias,
  updateLocationCapacity,
  type CreateLocationAliasRequestBody,
  type CreateLocationCapacityRequestBody,
  type CreateLocationRequestBody,
  type DeleteLocationAliasRequestBody,
  type DeleteLocationCapacityRequestBody,
  type DeleteLocationRequestBody,
  type ResolveLocationReviewItemRequestBody,
  type RetireLocationAliasRequestBody,
  type RetireLocationRequestBody,
  type UpdateLocationAliasRequestBody,
  type UpdateLocationCapacityRequestBody,
  type UpdateLocationRequestBody,
} from "@/lib/api/server";

const locationTypes = ["farm", "park", "shed", "cohort", "pen", "unknown"] as const;
const locationStatuses = ["active", "inactive", "staging", "review"] as const;
const aliasContexts = ["legacy_location_code", "legacy_bq_dashboard_shed", "legacy_bq_counts", "legacy_bq_mortality", "counts_source", "mortality_source", "manual", "import"] as const;
const capacityKinds = ["goat_occupancy", "quarantine", "feed_trial", "other"] as const;
const capacitySources = ["manual", "legacy_bq", "android_sop", "import"] as const;
const reviewStatuses = ["resolved", "dismissed"] as const;

export async function createLocationAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const initialAlias = initialAliasFromForm(formData);
    const initialCapacity = initialCapacityFromForm(formData);
    const body: CreateLocationRequestBody = {
      location_type: enumField(formData, "location_type", locationTypes),
      location_code: optionalString(formData, "location_code") ?? null,
      name: requiredString(formData, "name"),
      parent_location_id: optionalString(formData, "parent_location_id") ?? null,
      status: enumField(formData, "status", locationStatuses),
      country: optionalString(formData, "country") ?? "IN",
      state_region: optionalString(formData, "state_region") ?? null,
      district: optionalString(formData, "district") ?? null,
      pincode: optionalString(formData, "pincode") ?? null,
      timezone: optionalString(formData, "timezone") ?? "Asia/Kolkata",
      operational: operationalFromForm(formData),
    };
    const result = await createLocation(body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      const location = result.data.location;
      const associationErrors: string[] = [];
      if (initialAlias) {
        const aliasResult = await createLocationAlias(location.location_id, initialAlias, childIdempotencyKey(formData, "alias"));
        if (!aliasResult.ok) {
          associationErrors.push(`alias failed: ${actionErrorMessage(aliasResult.error)}`);
        }
      }
      if (initialCapacity) {
        const capacityResult = await createLocationCapacity(location.location_id, initialCapacity, childIdempotencyKey(formData, "capacity"));
        if (!capacityResult.ok) {
          associationErrors.push(`capacity failed: ${actionErrorMessage(capacityResult.error)}`);
        }
      }
      status = associationErrors.length > 0 ? "error" : "success";
      message =
        associationErrors.length > 0
          ? `${location.name} created, but ${associationErrors.join("; ")}`
          : `${location.name} created.`;
      formData.set("return_to", `/locations?location_id=${encodeURIComponent(location.location_id)}`);
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to create location.";
  }
  actionRedirect(formData, status, message);
}

export async function updateLocationAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: UpdateLocationRequestBody = {
      location_type: enumField(formData, "location_type", locationTypes),
      location_code: optionalString(formData, "location_code") ?? null,
      name: requiredString(formData, "name"),
      parent_location_id: optionalString(formData, "parent_location_id") ?? null,
      clear_parent: !optionalString(formData, "parent_location_id"),
      status: enumField(formData, "status", locationStatuses),
      country: optionalString(formData, "country") ?? "IN",
      state_region: optionalString(formData, "state_region") ?? null,
      district: optionalString(formData, "district") ?? null,
      pincode: optionalString(formData, "pincode") ?? null,
      timezone: optionalString(formData, "timezone") ?? "Asia/Kolkata",
      operational: operationalFromForm(formData),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await updateLocation(requiredString(formData, "location_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.location.name} updated.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to update location.";
  }
  actionRedirect(formData, status, message);
}

export async function retireLocationAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: RetireLocationRequestBody = {
      reason: requiredString(formData, "reason"),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await retireLocation(requiredString(formData, "location_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.location.name} retired.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to retire location.";
  }
  actionRedirect(formData, status, message);
}

export async function deleteLocationAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: DeleteLocationRequestBody = {
      reason: requiredString(formData, "reason"),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await deleteLocation(requiredString(formData, "location_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Location ${result.data.resource_id.slice(0, 8)} deleted.`;
      formData.set("return_to", "/locations");
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to delete location.";
  }
  actionRedirect(formData, status, message);
}

export async function createLocationAliasAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: CreateLocationAliasRequestBody = {
      alias_code: requiredString(formData, "alias_code"),
      source_context: enumField(formData, "source_context", aliasContexts),
      notes: optionalString(formData, "notes") ?? null,
    };
    const result = await createLocationAlias(requiredString(formData, "location_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.alias.alias_code} alias created.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to create alias.";
  }
  actionRedirect(formData, status, message);
}

export async function updateLocationAliasAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: UpdateLocationAliasRequestBody = {
      alias_code: requiredString(formData, "alias_code"),
      source_context: enumField(formData, "source_context", aliasContexts),
      notes: optionalString(formData, "notes") ?? null,
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await updateLocationAlias(
      requiredString(formData, "location_id"),
      requiredString(formData, "alias_id"),
      body,
      requiredString(formData, "idempotency_key"),
    );
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.alias.alias_code} alias updated.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to update alias.";
  }
  actionRedirect(formData, status, message);
}

export async function retireLocationAliasAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: RetireLocationAliasRequestBody = {
      reason: requiredString(formData, "reason"),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await retireLocationAlias(
      requiredString(formData, "location_id"),
      requiredString(formData, "alias_id"),
      body,
      requiredString(formData, "idempotency_key"),
    );
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.alias.alias_code} alias retired.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to retire alias.";
  }
  actionRedirect(formData, status, message);
}

export async function deleteLocationAliasAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: DeleteLocationAliasRequestBody = {
      reason: requiredString(formData, "reason"),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await deleteLocationAlias(
      requiredString(formData, "location_id"),
      requiredString(formData, "alias_id"),
      body,
      requiredString(formData, "idempotency_key"),
    );
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Alias ${result.data.resource_id.slice(0, 8)} deleted.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to delete alias.";
  }
  actionRedirect(formData, status, message);
}

export async function createLocationCapacityAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: CreateLocationCapacityRequestBody = {
      capacity_kind: enumField(formData, "capacity_kind", capacityKinds),
      capacity_value: requiredNumber(formData, "capacity_value"),
      effective_from: requiredString(formData, "effective_from"),
      effective_to: optionalString(formData, "effective_to") ?? null,
      source: enumField(formData, "source", capacitySources),
      source_ref: optionalString(formData, "source_ref") ?? null,
      notes: optionalString(formData, "notes") ?? null,
    };
    const result = await createLocationCapacity(requiredString(formData, "location_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.capacity.capacity_kind} capacity saved.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to create capacity.";
  }
  actionRedirect(formData, status, message);
}

export async function updateLocationCapacityAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: UpdateLocationCapacityRequestBody = {
      capacity_kind: enumField(formData, "capacity_kind", capacityKinds),
      capacity_value: requiredNumber(formData, "capacity_value"),
      effective_from: requiredString(formData, "effective_from"),
      effective_to: optionalString(formData, "effective_to") ?? null,
      source: enumField(formData, "source", capacitySources),
      source_ref: optionalString(formData, "source_ref") ?? null,
      notes: optionalString(formData, "notes") ?? null,
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await updateLocationCapacity(
      requiredString(formData, "location_id"),
      requiredString(formData, "capacity_record_id"),
      body,
      requiredString(formData, "idempotency_key"),
    );
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.capacity.capacity_kind} capacity updated.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to update capacity.";
  }
  actionRedirect(formData, status, message);
}

export async function deleteLocationCapacityAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: DeleteLocationCapacityRequestBody = {
      reason: requiredString(formData, "reason"),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await deleteLocationCapacity(
      requiredString(formData, "location_id"),
      requiredString(formData, "capacity_record_id"),
      body,
      requiredString(formData, "idempotency_key"),
    );
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Capacity ${result.data.resource_id.slice(0, 8)} deleted.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to delete capacity.";
  }
  actionRedirect(formData, status, message);
}

export async function resolveLocationReviewItemAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const nextStatus = enumField(formData, "status", reviewStatuses);
    const body: ResolveLocationReviewItemRequestBody = {
      status: nextStatus,
      canonical_location_id: nextStatus === "resolved" ? requiredString(formData, "canonical_location_id") : null,
      resolution_notes: requiredString(formData, "resolution_notes"),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await resolveLocationReviewItem(requiredString(formData, "review_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Review ${result.data.review_item.review_id.slice(0, 8)} ${result.data.review_item.status}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to resolve review item.";
  }
  actionRedirect(formData, status, message);
}

function operationalFromForm(formData: FormData): CreateLocationRequestBody["operational"] {
  return {
    usable_for_counts: optionalBoolean(formData, "usable_for_counts"),
    usable_for_feed: optionalBoolean(formData, "usable_for_feed"),
    usable_for_vaccination: optionalBoolean(formData, "usable_for_vaccination"),
    usable_for_sop: optionalBoolean(formData, "usable_for_sop"),
    is_holding: optionalBoolean(formData, "is_holding"),
    is_quarantine: optionalBoolean(formData, "is_quarantine"),
    is_icu: optionalBoolean(formData, "is_icu"),
    display_order: Number.parseInt(optionalString(formData, "display_order") ?? "0", 10) || 0,
    notes: optionalString(formData, "operational_notes") ?? null,
  };
}

function initialAliasFromForm(formData: FormData): CreateLocationAliasRequestBody | null {
  const aliasCode = optionalString(formData, "initial_alias_code");
  if (!aliasCode) return null;
  return {
    alias_code: aliasCode,
    source_context: enumField(formData, "initial_alias_context", aliasContexts),
    notes: optionalString(formData, "initial_alias_notes") ?? null,
  };
}

function initialCapacityFromForm(formData: FormData): CreateLocationCapacityRequestBody | null {
  const rawValue = optionalString(formData, "initial_capacity_value");
  if (!rawValue) return null;
  const capacityValue = Number.parseInt(rawValue, 10);
  if (!Number.isFinite(capacityValue) || capacityValue < 1) {
    throw new Error("initial capacity must be at least 1");
  }
  return {
    capacity_kind: enumField(formData, "initial_capacity_kind", capacityKinds),
    capacity_value: capacityValue,
    effective_from: requiredString(formData, "initial_capacity_from"),
    effective_to: optionalString(formData, "initial_capacity_to") ?? null,
    source: enumField(formData, "initial_capacity_source", capacitySources),
    source_ref: optionalString(formData, "initial_capacity_source_ref") ?? null,
    notes: optionalString(formData, "initial_capacity_notes") ?? null,
  };
}

function childIdempotencyKey(formData: FormData, suffix: string): string {
  const base = optionalString(formData, "idempotency_key") ?? randomUUID();
  return `${base}:${suffix}`;
}

function enumField<const T extends readonly string[]>(formData: FormData, key: string, values: T): T[number] {
  const value = requiredString(formData, key);
  if (!values.includes(value as T[number])) {
    throw new Error(`${key} is not supported`);
  }
  return value as T[number];
}
