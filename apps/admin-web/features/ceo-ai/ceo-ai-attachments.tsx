"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Stack from "@mui/material/Stack";
import Avatar from "@mui/material/Avatar";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";

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

/** An attachment on the composer tray or a sent turn: a template Chip (image thumbnail avatar or a
 *  file icon) that opens the viewer, with a delete action on the tray. */
export function Thumb({
  file,
  onOpen,
  onRemove,
}: {
  file: PreviewFile;
  onOpen: () => void;
  onRemove?: () => void;
}) {
  return (
    <Chip
      variant="outlined"
      label={file.name}
      title={file.name}
      aria-label={`Preview ${file.name}`}
      onClick={onOpen}
      onDelete={onRemove}
      avatar={isImage(file) ? <Avatar variant="rounded" alt={file.name} src={file.url} /> : undefined}
      icon={isImage(file) ? undefined : <Iconify icon="solar:file-text-bold" width={18} />}
      sx={{ maxWidth: 240 }}
    />
  );
}

// Full-screen viewer with prev/next for several files. Arrow keys slide, Esc closes. The MUI Dialog
// (fullScreen) portals it out of the chat panel, traps focus and gives it back to the chip that
// opened it; Escape stays on the capture listener below so it never also reaches the panel's own
// Escape (which would shrink the panel under the viewer).
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
    if (isImage(file)) return <Box component="img" src={file.url} alt={file.name} sx={{ maxWidth: 1, maxHeight: 1, objectFit: "contain" }} />;
    if (isPdf(file)) return <Box component="iframe" src={file.url} title={file.name} sx={{ width: 1, height: 1, border: 0, bgcolor: "background.paper" }} />;
    return (
      <Stack spacing={2} sx={{ alignItems: "center" }}>
        <Iconify icon="solar:file-text-bold" width={48} />
        <Typography variant="subtitle1">{file.name}</Typography>
        <Button variant="contained" color="primary" href={file.url} download={file.name}>
          Download
        </Button>
      </Stack>
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
        paper: { "aria-label": file.name } as object,
        transition: { onEntered: () => closeRef.current?.focus() },
      }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: "center", px: 2.5, py: 1.5, borderBottom: 1, borderColor: "divider" }}>
        <Typography variant="subtitle1" noWrap sx={{ flexGrow: 1 }}>
          {file.name}
        </Typography>
        {n > 1 ? (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {i + 1} / {n}
          </Typography>
        ) : null}
        {n > 1 ? (
          <>
            <IconButton aria-label="Previous" onClick={() => setI((v) => (v - 1 + n) % n)}>
              <Iconify icon="eva:arrow-ios-back-fill" />
            </IconButton>
            <IconButton aria-label="Next" onClick={() => setI((v) => (v + 1) % n)}>
              <Iconify icon="eva:arrow-ios-forward-fill" />
            </IconButton>
          </>
        ) : null}
        <IconButton ref={closeRef} onClick={onClose} aria-label="Close preview">
          <Iconify icon="mingcute:close-line" />
        </IconButton>
      </Stack>
      <Box sx={{ flex: "1 1 auto", minHeight: 0, p: 2.5, display: "grid", placeItems: "center" }}>{body}</Box>
    </Dialog>
  );
}
