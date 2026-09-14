"use client";

import { X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

export type LightboxItem = {
  proofRef: string;
  url: string;
  kind: "photo" | "video";
  /** Slot title, shown under the tile and over the enlarged capture. */
  title: string;
};

/**
 * An animal's captures as a strip of uniform tiles, each opening big in a client-local lightbox
 * (maintainer ask 2026-09-14: every photo and video visible on the card, a click shows it big).
 * Open/close is purely local state: no navigation, no query parameter, no request. Escape, the
 * scrim, and the X all close it and focus returns to the tile that opened it. The strip never
 * loads remote proof bytes by itself; photos and videos fetch only after the reviewer opens one.
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
      {items.map((item) => (
        <figure key={item.proofRef} className="ap-tile">
          <button
            type="button"
            className={item.kind === "photo" ? "ap-tile-btn" : "ap-tile-btn video"}
            aria-label={`${openLabel}: ${item.title}`}
            title={openLabel}
            onClick={(event) => {
              openerRef.current = event.currentTarget;
              setOpenRef(item.proofRef);
            }}
          >
            {item.kind === "photo" ? (
              <span className="ap-tile-placeholder" aria-hidden="true">
                {item.title}
              </span>
            ) : (
              <>
                <span className="ap-tile-play" aria-hidden="true">
                  ▶
                </span>
              </>
            )}
          </button>
          <figcaption className="muted small">{item.title}</figcaption>
        </figure>
      ))}
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
