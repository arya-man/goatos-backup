"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, FileText, X } from "lucide-react";
import Dialog from "@mui/material/Dialog";

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

export function Thumb({ file, onOpen, onRemove }: { file: PreviewFile; onOpen: () => void; onRemove?: () => void }) {
  return (
    <span className={`mzai-thumb${isImage(file) ? " img" : ""}`}>
      <button type="button" className="mzai-thumb-open" onClick={onOpen} title={file.name} aria-label={`Preview ${file.name}`}>
        {isImage(file) ? (
          // eslint-disable-next-line @next/next/no-img-element -- local object URL preview
          <img src={file.url} alt={file.name} />
        ) : (
          <>
            <FileText size={16} />
            <span className="mzai-thumb-name">{file.name}</span>
          </>
        )}
      </button>
      {onRemove ? (
        <button type="button" className="mzai-thumb-x" onClick={onRemove} aria-label={`Remove ${file.name}`}>
          <X size={11} />
        </button>
      ) : null}
    </span>
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
      <div className="mzai-lb-card">
        <FileText size={40} />
        <span>{file.name}</span>
        <a href={file.url} download={file.name}>
          Download
        </a>
      </div>
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
        paper: { className: "mzai-lb", "aria-label": file.name, onClick: onClose } as object,
        transition: { onEntered: () => closeRef.current?.focus() },
      }}
    >
      <div className="mzai-lb-top" onClick={(e) => e.stopPropagation()}>
        <span className="mzai-lb-name">{file.name}</span>
        {n > 1 ? <span className="mzai-lb-count">{i + 1} / {n}</span> : null}
        <button ref={closeRef} type="button" onClick={onClose} aria-label="Close preview">
          <X size={18} />
        </button>
      </div>
      <div className="mzai-lb-body" onClick={(e) => e.stopPropagation()}>
        {body}
      </div>
      {n > 1 ? (
        <>
          <button type="button" className="mzai-lb-nav prev" aria-label="Previous" onClick={(e) => { e.stopPropagation(); setI((v) => (v - 1 + n) % n); }}>
            <ChevronLeft size={26} />
          </button>
          <button type="button" className="mzai-lb-nav next" aria-label="Next" onClick={(e) => { e.stopPropagation(); setI((v) => (v + 1) % n); }}>
            <ChevronRight size={26} />
          </button>
        </>
      ) : null}
    </Dialog>
  );
}
