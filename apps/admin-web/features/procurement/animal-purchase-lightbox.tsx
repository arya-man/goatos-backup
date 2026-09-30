"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { faro } from "@grafana/faro-web-sdk";
import Box from "@mui/material/Box";
import ButtonBase from "@mui/material/ButtonBase";
import Dialog from "@mui/material/Dialog";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import useMediaQuery from "@mui/material/useMediaQuery";
import { useTheme } from "@mui/material/styles";
import { varAlpha } from "minimal-shared/utils";
import { Iconify } from "@/components/minimal/iconify";
import { MEDIA_TILE_CAPTION_SX, MEDIA_TILE_FIGURE_SX, MEDIA_TILE_SIZE } from "./animal-purchase-tile-sx";
import { TAP_MIN } from "@/theme/tap-target";

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
 * Open/close is purely local state: no navigation, no query parameter, no request. The lightbox is
 * the template MUI Dialog (portalled, full screen on phones): Escape, the backdrop and the X all
 * close it and focus returns to the tile that opened it. The strip never loads original video
 * bytes by itself; bounded photo/poster previews attach only when the tile is in or just ahead of
 * the viewport, and full media fetches only after the reviewer opens one.
 */
export function AnimalPurchaseLightbox({ items, openLabel, closeLabel }: { items: LightboxItem[]; openLabel: string; closeLabel: string }) {
  const [openRef, setOpenRef] = useState<string | null>(null);
  const [previewsEnabled, setPreviewsEnabled] = useState(false);
  // A thumbnail that fails to load (expired signed URL, missing poster) shows the titled
  // placeholder tile instead of the browser's broken-image glyph.
  const [broken, setBroken] = useState<ReadonlySet<string>>(() => new Set());
  const stripRef = useRef<HTMLDivElement | null>(null);
  const open = items.find((item) => item.proofRef === openRef) ?? null;
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down("sm"));

  const close = useCallback(() => setOpenRef(null), []);

  useEffect(() => {
    if (!open) return;
    try {
      faro.api?.pushEvent("animal_purchase_proof_media_opened", {
        proof_ref: open.proofRef,
        kind: open.kind,
      });
    } catch {
      // Faro must never break proof review.
    }
  }, [open]);

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

  return (
    // `display: contents`: the tiles sit in the caller's tile row beside its placeholder tiles.
    <Box ref={stripRef} sx={{ display: "contents" }}>
      {items.map((item, index) => {
        const shouldLoadPreview = previewsEnabled && item.thumbnailUrl && !broken.has(item.proofRef);
        return (
          <Box component="figure" key={`${item.proofRef}-${index}`} sx={MEDIA_TILE_FIGURE_SX}>
            <ButtonBase
              aria-label={`${openLabel}: ${item.title}`}
              title={openLabel}
              onClick={() => setOpenRef(item.proofRef)}
              sx={{
                position: "relative",
                display: "block",
                width: MEDIA_TILE_SIZE,
                height: MEDIA_TILE_SIZE,
                border: 1,
                borderColor: "divider",
                borderRadius: 1.5,
                overflow: "hidden",
                bgcolor: "background.neutral",
                cursor: "zoom-in",
                "&:hover, &.Mui-focusVisible": {
                  borderColor: "primary.light",
                  boxShadow: `0 0 0 2px ${varAlpha(theme.vars.palette.primary.mainChannel, 0.35)}`,
                },
                "& img": { display: "block", width: 1, height: 1, objectFit: "cover", pointerEvents: "none", bgcolor: "grey.900" },
              }}
            >
              {shouldLoadPreview ? (
                // admin-proof-media-egress:ignore bounded animal-purchase tile preview: IntersectionObserver attaches only visible/lookahead photo or backend poster URLs; original video URLs are never used here.
                // eslint-disable-next-line @next/next/no-img-element
                <img src={item.thumbnailUrl} alt="" data-tile-preview="" loading="lazy" decoding="async" onError={() => setBroken((prev) => new Set(prev).add(item.proofRef))} />
              ) : (
                <Typography
                  component="span"
                  variant="caption"
                  aria-hidden="true"
                  sx={{ display: "grid", placeItems: "center", width: 1, height: 1, p: 1, color: "text.secondary", textAlign: "center", lineHeight: 1.2 }}
                >
                  {item.title}
                </Typography>
              )}
              {item.kind === "video" ? (
                <Box component="span" aria-hidden="true" sx={{ position: "absolute", inset: 0, zIndex: 1, display: "grid", placeItems: "center" }}>
                  <Box
                    component="span"
                    sx={{
                      display: "grid",
                      placeItems: "center",
                      width: "calc(5 * var(--spacing))",
                      height: "calc(5 * var(--spacing))",
                      borderRadius: "50%",
                      color: "common.white",
                      bgcolor: varAlpha(theme.vars.palette.common.blackChannel, 0.45),
                      border: `1px solid ${varAlpha(theme.vars.palette.common.whiteChannel, 0.35)}`,
                    }}
                  >
                    <Iconify icon="solar:play-circle-bold" width={24} />
                  </Box>
                </Box>
              ) : null}
            </ButtonBase>
            <Typography component="figcaption" variant="caption" sx={MEDIA_TILE_CAPTION_SX}>
              {item.title}
            </Typography>
          </Box>
        );
      })}
      <Dialog
        open={open !== null}
        onClose={close}
        fullScreen={fullScreen}
        maxWidth="lg"
        aria-label={open?.title}
        slotProps={{ paper: { sx: { bgcolor: "background.paper" } } }}
      >
        {open ? (
          <>
            <DialogTitle sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 1.5, py: 1.5, pr: 1.5 }}>
              <Typography component="span" variant="subtitle1" sx={{ minWidth: 0, overflowWrap: "anywhere" }}>
                {open.title}
              </Typography>
              <IconButton aria-label={closeLabel} title={closeLabel} onClick={close} sx={{ width: TAP_MIN, height: TAP_MIN, flexShrink: 0 }}>
                <Iconify icon="mingcute:close-line" />
              </IconButton>
            </DialogTitle>
            <DialogContent
              sx={{
                display: "grid",
                placeItems: "center",
                pb: 2,
                "& img, & video": {
                  display: "block",
                  maxWidth: 1,
                  maxHeight: fullScreen ? "calc(100dvh - 96px)" : "calc(94dvh - 96px)",
                  objectFit: "contain",
                  borderRadius: 1,
                  bgcolor: "common.black",
                },
              }}
            >
              {open.kind === "photo" ? (
                // admin-proof-media-egress:ignore reviewer explicitly opened this one animal-purchase proof; telemetry above attributes the selected proofRef/kind and no list tile loads bytes.
                // eslint-disable-next-line @next/next/no-img-element
                <img src={open.url} alt="" data-lightbox-media="" />
              ) : (
                // admin-proof-media-egress:ignore reviewer explicitly opened this one animal-purchase proof; telemetry above attributes the selected proofRef/kind and no list tile loads bytes.
                <video src={open.url} controls autoPlay playsInline data-lightbox-media="" />
              )}
            </DialogContent>
          </>
        ) : null}
      </Dialog>
    </Box>
  );
}
