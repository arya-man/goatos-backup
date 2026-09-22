"use client";

import { useRouter } from "next/navigation";
import { useRef, useState, useTransition } from "react";
import { Pencil } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { openRegisterDraft } from "./health-register-actions";
import { mintKey } from "./health-register-keys";

/**
 * Open the editor on a class's register.
 *
 * It is a BUTTON running a server action rather than a link, because opening the editor WRITES: it
 * creates the draft from the live register when none is open. The navigation happens here, in the
 * client, after the action returns — a `redirect()` inside a server action invoked from an event
 * handler is swallowed by the transition, which is the "Edit does nothing" bug the protocol half
 * already hit.
 */
export function OpenRegisterDraftButton({
  animalClass,
  pageContract,
  enabled,
  disabledReason,
  basePath,
  openable,
}: {
  animalClass: string;
  pageContract: AdminUiPageContract;
  enabled: boolean;
  disabledReason: string;
  basePath: string;
  openable: boolean;
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string>("");
  // One key per human INTENT, minted on the first press and reused across retries of that same
  // press, then rotated once it succeeds. That is what makes a network-failed open safe to press
  // again: the replay returns the draft that was already created instead of opening a second one.
  //
  // It is minted in the HANDLER, never during render. A key generated while rendering changes on
  // every re-render, so a retry after a failure would carry a different key and write twice --
  // which is the exact opposite of what the key is for.
  const key = useRef<string>("");

  const disabled = !enabled || !openable || pending;
  const title = !enabled ? disabledReason : !openable ? copy(pageContract, "empty.registers") : "";

  return (
    <>
      <button
        type="button"
        className="btn ghost"
        disabled={disabled}
        title={title}
        onClick={() =>
          startTransition(async () => {
            if (!key.current) key.current = mintKey(`register-open-${animalClass}`);
            const result = await openRegisterDraft(animalClass, key.current);
            if (!result.ok) {
              setError(result.detail ?? copy(pageContract, "action.error_backend"));
              return;
            }
            key.current = "";
            const sep = basePath.includes("?") ? "&" : "?";
            router.push(`${basePath}${sep}hc_register=${encodeURIComponent(result.versionId ?? "")}`);
          })
        }
      >
        <Pencil className="ic" aria-hidden="true" /> {copy(pageContract, "action.edit_register")}
      </button>
      {error ? (
        <div className="small" style={{ color: "var(--danger)" }}>
          {error}
        </div>
      ) : null}
    </>
  );
}
