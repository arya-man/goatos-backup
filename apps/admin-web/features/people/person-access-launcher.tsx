"use client";

import { KeyRound } from "lucide-react";
import { useCallback, useState, useTransition } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { PersonAccess } from "@/lib/api/server";
import { loadPersonAccessAction } from "./access-actions";
import { PersonAccessModal } from "./person-access-modal";

/**
 * Opens the access editor for one directory row.
 *
 * The overlay opens IMMEDIATELY and loads its own detail, rather than the page
 * fetching access for every row it renders. Two reasons, both rules rather than
 * preferences: an overlay toggle must not navigate or trigger a page-level load
 * (the local-overlay contract), and pre-loading access for 25 rows would be 25
 * reads to render one screen nobody may open.
 */
export function PersonAccessLauncher({
  personId,
  personName,
  pageContract,
}: {
  personId: string;
  personName: string;
  pageContract: AdminUiPageContract;
}) {
  const [access, setAccess] = useState<PersonAccess | null>(null);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();

  const openEditor = useCallback(() => {
    setOpen(true);
    setError("");
    startTransition(async () => {
      const result = await loadPersonAccessAction(personId);
      if (!result.ok) {
        setError(result.message);
        return;
      }
      setAccess(result.access);
    });
  }, [personId]);

  const close = useCallback(() => {
    setOpen(false);
    // The loaded record is DISCARDED on close, so reopening always re-reads. Access
    // is exactly the state where a cached view can be quietly out of date after
    // someone else's edit.
    setAccess(null);
    setError("");
  }, []);

  return (
    <>
      <button
        type="button"
        className="btn sm ghost"
        onClick={openEditor}
        title={copy(pageContract, "access.open_hint")}
        aria-label={`${copy(pageContract, "access.open")} — ${personName}`}
      >
        <KeyRound size={13} aria-hidden />
        {copy(pageContract, "access.open")}
      </button>

      {open && access ? (
        <PersonAccessModal access={access} pageContract={pageContract} onClose={close} />
      ) : null}

      {open && !access ? (
        <div className="vr-modal-scrim on" onMouseDown={close}>
          <div className="vr-modal on" role="dialog" aria-modal="true" aria-label={personName}>
            <div className="vr-modal-bd">
              {error ? (
                <div className="alert" role="alert">
                  {error}
                </div>
              ) : (
                <div className="bd" aria-live="polite">
                  {pending ? copy(pageContract, "access.loading") : copy(pageContract, "access.error.load")}
                </div>
              )}
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}
