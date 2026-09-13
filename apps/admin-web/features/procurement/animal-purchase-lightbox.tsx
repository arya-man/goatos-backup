"use client";

import { X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

export type LightboxItem = {
  proofRef: string;
  url: string;
  kind: "photo" | "video";
  /** Slot title, shown over the enlarged capture. */
  title: string;
};

/**
 * Client-local lightbox for an animal's captures (maintainer ask 2026-09-14: every photo and
 * video visible on the card, and a click shows it big). Open/close is purely local state: no
 * navigation, no query parameter, no request. Escape, the scrim, and the X all close it and focus
 * returns to the thumbnail that opened it.
 */
export function AnimalPurchaseLightbox({ items, openLabel, closeLabel }: { items: LightboxItem[]; openLabel: string; closeLabel: string }) {
  const [openRef, setOpenRef] = useState<string | null>(null);
  const openerRef = useRef<HTMLElement | null>(null);
  const closeRef = useRef<HTMLButtonElement | null>(null);
  const open = items.find((item) => item.proofRef === openRef) ?? null;

  const close = useCallback(() => {
    setOpenRef(null);
    openerRef.current?.focus();
  }, []);

  useEffect(() => {
    if (!open) return;
    closeRef.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, close]);

  return (
    <>
      {items.map((item) =>
        item.kind === "photo" ? (
          <button
            key={item.proofRef}
            type="button"
            className="ap-sop-photo"
            aria-label={openLabel}
            title={openLabel}
            onClick={(event) => {
              openerRef.current = event.currentTarget;
              setOpenRef(item.proofRef);
            }}
          >
            {/* A signed proof link, not an optimizable static asset -- same as the verification drawer. */}
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src={item.url} alt="" loading="lazy" />
          </button>
        ) : (
          <div key={item.proofRef} className="vr-player ap-sop-video">
            {/* Metadata only until the reviewer presses play; the expand button opens it big. */}
            <video src={item.url} controls preload="metadata" playsInline />
            <button
              type="button"
              className="ap-sop-expand"
              aria-label={openLabel}
              title={openLabel}
              onClick={(event) => {
                openerRef.current = event.currentTarget;
                setOpenRef(item.proofRef);
              }}
            >
              ⤢
            </button>
          </div>
        ),
      )}
      {open ? (
        <div className="ap-lightbox" role="dialog" aria-modal="true" aria-label={open.title}>
          <button type="button" className="ap-lightbox-scrim" aria-label={closeLabel} onClick={close} />
          <div className="ap-lightbox-body">
            <div className="ap-lightbox-head">
              <b>{open.title}</b>
              <button ref={closeRef} type="button" className="iconbtn" aria-label={closeLabel} title={closeLabel} onClick={close}>
                <X size={16} />
              </button>
            </div>
            {open.kind === "photo" ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img src={open.url} alt="" className="ap-lightbox-media" />
            ) : (
              <video src={open.url} controls autoPlay playsInline className="ap-lightbox-media" />
            )}
          </div>
        </div>
      ) : null}
    </>
  );
}
