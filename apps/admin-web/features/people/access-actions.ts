"use server";

// Write and detail-fetch path for the per-person access editor.
//
// Server Actions rather than a Route Handler, and rather than a plain fetch from
// the client: both calls are authenticated through the server config, and the
// overlay must not navigate or trigger a page-level load to read its own detail
// (the local-overlay contract in AGENTS.md).
import { revalidatePath } from "next/cache";

import {
  getDesignationDefaults,
  getWorkforcePersonAccess,
  saveWorkforcePersonAccess,
  type AccessModuleWrite,
  type PersonAccess,
  type SavePersonAccessRequest,
} from "@/lib/api/server";

const PEOPLE_PATH = "/people";

export type SaveAccessResult = { ok: true } | { ok: false; message: string };

/**
 * Replace one person's access.
 *
 * The failure message is rendered VERBATIM by the editor, so it must stay the
 * backend's farm-worded reason ("Feed is not available on the phone") rather than
 * a generic banner — the admin needs to know which tick was refused, and why.
 */
export async function savePersonAccessAction({
  personId,
  body,
}: {
  personId: string;
  body: SavePersonAccessRequest;
}): Promise<SaveAccessResult> {
  const result = await saveWorkforcePersonAccess(personId, body);
  if (!result.ok) {
    return {
      ok: false,
      message:
        result.error.message ||
        "That could not be saved. Reload the page to see the current settings, then try again.",
    };
  }
  revalidatePath(PEOPLE_PATH);
  return { ok: true };
}

export type DesignationDefaultsResult =
  | { ok: true; modules: AccessModuleWrite[] }
  | { ok: false; message: string };

/** What picking a designation pre-fills. One call, not one per module. */
export async function designationDefaultsAction(code: string): Promise<DesignationDefaultsResult> {
  const result = await getDesignationDefaults(code);
  if (!result.ok) {
    return {
      ok: false,
      message: "Those defaults could not be loaded. Set the access by hand, or try again.",
    };
  }
  return { ok: true, modules: result.data.modules };
}

export type LoadAccessResult = { ok: true; access: PersonAccess } | { ok: false; message: string };

/**
 * Read one person's access for the overlay. Called AFTER the overlay is already
 * open, so opening it is instant and only the detail waits — the list row has no
 * access data to open from, and a page-level fetch would make an overlay toggle
 * feel like a navigation.
 */
export async function loadPersonAccessAction(personId: string): Promise<LoadAccessResult> {
  const result = await getWorkforcePersonAccess(personId);
  if (!result.ok) {
    return {
      ok: false,
      message:
        result.error.code === "person_not_found"
          ? "That person is no longer on the roster."
          : "Their access could not be loaded. Close this and try again.",
    };
  }
  return { ok: true, access: result.data };
}
