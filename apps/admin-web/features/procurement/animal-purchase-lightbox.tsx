"use client";

import { X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { faro } from "@grafana/faro-web-sdk";

export type LightboxItem = {
  proofRef: string;
  url: string;
  thumbnailUrl?: string;
  kind: "photo" | "video";
  /** Slot title, shown under the tile and over the enlarged capture. */
  title: string;
};

/**
 * An animal's captures as a strip of uniform tiles, each opening big in a client-local lightbox
 * (maintainer ask 2026-09-14: every photo and video visible on the card, a click shows it big).
 * Open/close is purely local state: no navigation, no query parameter, no request. Escape, the
 * scrim, and the X all close it and focus returns to the tile that opened it. The strip never
 * loads original video bytes by itself; bounded photo/poster previews attach only when the tile is
 * in or just ahead of the viewport, and full media fetches only after the reviewer opens one.
 */
export function AnimalPurchaseLightbox({ items, openLabel, closeLabel }: { items: LightboxItem[]; openLabel: string; closeLabel: string }) {
  const [openRef, setOpenRef] = useState<string | null>(null);
  const [previewsEnabled, setPreviewsEnabled] = useState(false);
  const openerRef = useRef<HTMLElement | null>(null);
  const stripRef = useRef<HTMLDivElement | null>(null);
  const closeRef = useRef<HTMLButtonElement | null>(null);
  const open = items.find((item) => item.proofRef === openRef) ?? null;

  const close = useCallback(() => {
    setOpenRef(null);
    openerRef.current?.focus();
  }, []);

  useEffect(() => {
    if (!open) return;
    closeRef.current?.focus();
    try {
      faro.api?.pushEvent("animal_purchase_proof_media_opened", {
        proof_ref: open.proofRef,
        kind: open.kind,
      });
    } catch {
      // Faro must never break proof review.
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, close]);

  useEffect(() => {
    if (!items.some((item) => item.thumbnailUrl)) return;
    const row = stripRef.current?.closest(".ap-animal");
    if (!(row instanceof Element)) return;
    if (typeof IntersectionObserver === "undefined") {
      const timeout = window.setTimeout(() => setPreviewsEnabled(true), 0);
      return () => window.clearTimeout(timeout);
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) setPreviewsEnabled(true);
      },
      { root: null, rootMargin: "320px 0px", threshold: 0.01 },
    );
    observer.observe(row);
    return () => observer.disconnect();
  }, [items]);

  // A thumbnail that fails to load (expired signed URL, missing poster) shows the titled
  // placeholder tile instead of the browser's broken-image glyph. Error events do not bubble, so
  // the strip listens in the capture phase rather than each tile carrying a handler.
  useEffect(() => {
    const strip = stripRef.current;
    if (!strip) return;
    const onError = (event: Event) => {
      const img = event.target;
      if (!(img instanceof HTMLImageElement) || !img.classList.contains("ap-tile-preview")) return;
      img.style.display = "none";
      img.closest(".ap-tile")?.classList.add("ap-tile-broken");
    };
    strip.addEventListener("error", onError, true);
    return () => strip.removeEventListener("error", onError, true);
  }, []);

  return (
    <div ref={stripRef} className="ap-lightbox-strip">
      {items.map((item, index) => {
        const shouldLoadPreview = previewsEnabled && item.thumbnailUrl;
        return (
          <figure key={`${item.proofRef}-${index}`} className="ap-tile">
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
              {shouldLoadPreview ? (
                // admin-proof-media-egress:ignore bounded animal-purchase tile preview: IntersectionObserver attaches only visible/lookahead photo or backend poster URLs; original video URLs are never used here.
                // eslint-disable-next-line @next/next/no-img-element
                <img src={item.thumbnailUrl} alt="" className="ap-tile-preview" loading="lazy" decoding="async" />
              ) : (
                <span className="ap-tile-placeholder" aria-hidden="true">
                  {item.title}
                </span>
              )}
              {item.kind === "video" ? (
                <span className="ap-tile-play" aria-hidden="true">
                  ▶
                </span>
              ) : null}
            </button>
            <figcaption className="muted small">{item.title}</figcaption>
          </figure>
        );
      })}
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
              // admin-proof-media-egress:ignore reviewer explicitly opened this one animal-purchase proof; telemetry above attributes the selected proofRef/kind and no list tile loads bytes.
              // eslint-disable-next-line @next/next/no-img-element
              <img src={open.url} alt="" className="ap-lightbox-media" />
            ) : (
              // admin-proof-media-egress:ignore reviewer explicitly opened this one animal-purchase proof; telemetry above attributes the selected proofRef/kind and no list tile loads bytes.
              <video src={open.url} controls autoPlay playsInline className="ap-lightbox-media" />
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}
