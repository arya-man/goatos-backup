"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import Dialog from "@mui/material/Dialog";
import ButtonBase from "@mui/material/ButtonBase";
import Box from "@mui/material/Box";
import { Iconify } from "@/components/minimal/iconify";
import {
  lightboxBodySx,
  lightboxCardSx,
  lightboxCountSx,
  lightboxNameSx,
  lightboxNavSx,
  lightboxPaperSx,
  lightboxTopSx,
  thumbNameSx,
  thumbOpenSx,
  thumbRemoveSx,
  thumbSx,
} from "./ceo-ai-styles";

// A file shown in the composer tray or on a sent message. `url` is an object URL
// (images/PDFs preview inline); other types show a file card.
export type PreviewFile = { name: string; type: string; url: string };

export function toPreview(file: File): PreviewFile {
  return { name: file.name, type: file.type, url: URL.createObjectURL(file) };
}

// revokePreviews releases object URLs (server-backed URLs are left alone).
export function revokePreviews(files: PreviewFile[] | undefined): void {
  files?.forEach((f) => {
    if (f.url.startsWith("blob:")) URL.revokeObjectURL(f.url);
  });
}

const isImage = (f: PreviewFile) => f.type.startsWith("image/");
const isPdf = (f: PreviewFile) => f.type === "application/pdf";

// Screenshots from retina screens are large; downscale images to <= 2000px JPEG/PNG
// before upload so the request stays small and the model reads them faster.
export async function shrinkImage(file: File, maxSide = 2000): Promise<File> {
  if (!file.type.startsWith("image/") || file.type === "image/gif" || file.type === "image/svg+xml") return file;
  if (typeof createImageBitmap !== "function") return file;
  const bitmap = await createImageBitmap(file).catch(() => null);
  if (!bitmap) return file;
  const scale = Math.min(1, maxSide / Math.max(bitmap.width, bitmap.height));
  if (scale === 1 && file.size < 1_500_000) {
    bitmap.close();
    return file;
  }
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(bitmap.width * scale);
  canvas.height = Math.round(bitmap.height * scale);
  const ctx = canvas.getContext("2d");
  // No 2D context (memory-starved webview): upload the original, never a blank image.
  if (!ctx) {
    bitmap.close();
    return file;
  }
  ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  const type = file.type === "image/png" && file.size < 3_000_000 ? "image/png" : "image/jpeg";
  const blob = await new Promise<Blob | null>((r) => canvas.toBlob(r, type, 0.88));
  if (!blob) return file;
  return new File([blob], file.name.replace(/\.\w+$/, type === "image/jpeg" ? ".jpg" : ".png"), { type });
}

export function Thumb({
  file,
  onOpen,
  onRemove,
  inUserMessage = false,
}: {
  file: PreviewFile;
  onOpen: () => void;
  onRemove?: () => void;
  /** On a sent user message the card sits on the paper surface (the bubble is tinted). */
  inUserMessage?: boolean;
}) {
  return (
    <Box component="span" sx={thumbSx}>
      <ButtonBase sx={thumbOpenSx(isImage(file), inUserMessage)} onClick={onOpen} title={file.name} aria-label={`Preview ${file.name}`}>
        {isImage(file) ? (
          // eslint-disable-next-line @next/next/no-img-element -- local object URL preview
          <img src={file.url} alt={file.name} />
        ) : (
          <>
            <Iconify icon="solar:file-text-bold" width={16} />
            <Box component="span" sx={thumbNameSx}>{file.name}</Box>
          </>
        )}
      </ButtonBase>
      {onRemove ? (
        <ButtonBase sx={thumbRemoveSx} onClick={onRemove} aria-label={`Remove ${file.name}`}>
          <Iconify icon="mingcute:close-line" width={11} />
        </ButtonBase>
      ) : null}
    </Box>
  );
}

// Full-screen viewer with prev/next for several files. Arrow keys slide, Esc closes. The template
// MUI Dialog (fullScreen) portals it out of the chat panel, traps focus and gives it back to the
// thumbnail that opened it; Escape stays on the capture listener below so it never also reaches
// the panel's own Escape (which would shrink the panel under the viewer).
export function Lightbox({ files, start, onClose }: { files: PreviewFile[]; start: number; onClose: () => void }) {
  const [i, setI] = useState(start);
  const n = files.length;
  const file = files[i];
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        onClose();
        return;
      }
      if (n < 2) return;
      if (e.key === "ArrowRight") {
        e.preventDefault();
        setI((v) => (v + 1) % n);
      }
      if (e.key === "ArrowLeft") {
        e.preventDefault();
        setI((v) => (v - 1 + n) % n);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [n, onClose]);
  const body = useMemo(() => {
    if (!file) return null;
    if (isImage(file)) {
      // eslint-disable-next-line @next/next/no-img-element -- local object URL preview
      return <img src={file.url} alt={file.name} />;
    }
    if (isPdf(file)) return <iframe src={file.url} title={file.name} />;
    return (
      <Box sx={lightboxCardSx}>
        <Iconify icon="solar:file-text-bold" width={40} />
        <span>{file.name}</span>
        <a href={file.url} download={file.name}>
          Download
        </a>
      </Box>
    );
  }, [file]);
  if (!file) return null;
  return (
    <Dialog
      open
      fullScreen
      // Escape is handled by the capture listener above (so it never reaches the panel).
      onClose={(_event, reason) => {
        if (reason !== "escapeKeyDown") onClose();
      }}
      slotProps={{
        paper: { sx: lightboxPaperSx, "aria-label": file.name, onClick: onClose } as object,
        transition: { onEntered: () => closeRef.current?.focus() },
      }}
    >
      <Box sx={lightboxTopSx} onClick={(e) => e.stopPropagation()}>
        <Box component="span" sx={lightboxNameSx}>{file.name}</Box>
        {n > 1 ? <Box component="span" sx={lightboxCountSx}>{i + 1} / {n}</Box> : null}
        <ButtonBase ref={closeRef} onClick={onClose} aria-label="Close preview">
          <Iconify icon="mingcute:close-line" width={18} />
        </ButtonBase>
      </Box>
      <Box sx={lightboxBodySx} onClick={(e) => e.stopPropagation()}>
        {body}
      </Box>
      {n > 1 ? (
        <>
          <ButtonBase sx={lightboxNavSx("prev")} aria-label="Previous" onClick={(e) => { e.stopPropagation(); setI((v) => (v - 1 + n) % n); }}>
            <Iconify icon="eva:arrow-ios-back-fill" width={26} />
          </ButtonBase>
          <ButtonBase sx={lightboxNavSx("next")} aria-label="Next" onClick={(e) => { e.stopPropagation(); setI((v) => (v + 1) % n); }}>
            <Iconify icon="eva:arrow-ios-forward-fill" width={26} />
          </ButtonBase>
        </>
      ) : null}
    </Dialog>
  );
}
