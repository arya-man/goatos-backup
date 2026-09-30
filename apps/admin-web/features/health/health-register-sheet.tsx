"use client";

import { useRef, useState } from "react";
import { InfoTip } from "@/components/app/info-tip";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";

import { Iconify } from "@/components/minimal/iconify";

import { copy, optionalCopy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import {
  IDEMPOTENCY_HEADER,
  TEMPLATE_CLASS_PLACEHOLDER,
  registerSheetHref,
  registerSheetImportUrl,
} from "@/lib/sheet-upload";

import { mintKey } from "./health-register-keys";


type Problem = { path?: string; message: string; fatal?: boolean };

/**
 * Download a type's rulebook, edit it, upload it back.
 *
 * Maintainer instruction 2026-09-23: forty questions and thirty illnesses typed one field at a
 * time through a form is how a new type stays half-authored for a month.
 *
 * THE UPLOAD WRITES A DRAFT, never a publish, and the copy says so before the file is chosen
 * rather than after it lands. The draft appears on this same tab where the author reads it, and
 * publishing runs the two-direction check every hand edit runs -- a question no rule reads, a
 * rule reading a finding no question asks. That gate is what makes accepting a spreadsheet safe.
 *
 * Downloads are real anchors: a file download is exactly what an anchor is for, it works without
 * JavaScript, and middle-click still does the expected thing. The upload is a form post through
 * the same-origin proxy, which streams both ways so a sheet never sits in this component.
 */
export function RegisterSheetControls({
  animalClass,
  typeLabel,
  pageContract,
  mayWrite,
  disabledReason,
  onImported,
}: {
  animalClass: string;
  typeLabel: string;
  pageContract: AdminUiPageContract;
  mayWrite: boolean;
  disabledReason: string;
  onImported?: () => void;
}) {
  const fileInput = useRef<HTMLInputElement | null>(null);
  const intentKey = useRef<string>("");
  // WHICH file the live key belongs to. The key is per chosen FILE, not per component: a network
  // retry of the same file must reuse it, while a corrected file is a new intent.
  const intentFile = useRef<string>("");
  const [busy, setBusy] = useState(false);
  const [problems, setProblems] = useState<Problem[]>([]);
  const [note, setNote] = useState("");

  const href = (action: "template" | "export", format: "csv" | "xlsx" | "json") =>
    registerSheetHref(animalClass, action, format);

  async function upload(file: File) {
    setBusy(true);
    setProblems([]);
    setNote("");
    // One key per human INTENT -- THIS chosen file -- reused if the upload is retried after a
    // network failure, so a repeat cannot write the draft twice.
    //
    // It is keyed on the file's own identity rather than minted once, because the key is cleared
    // only ON SUCCESS: an upload that committed server-side but whose response never arrived
    // leaves the key live, and the author's NEXT upload is usually the corrected sheet. Reusing
    // the key for it now earns a 409 from the backend (the fingerprint carries the document), and
    // a conflict the author cannot clear is no better than the silent replay it replaced. A
    // different file is a different intent and gets its own key.
    const fileIdentity = `${file.name}:${file.size}:${file.lastModified}`;
    if (!intentKey.current || intentFile.current !== fileIdentity) {
      intentKey.current = mintKey(`register-sheet-${animalClass}`);
      intentFile.current = fileIdentity;
    }
    try {
      const body = new FormData();
      body.append("file", file);
      const res = await fetch(registerSheetImportUrl(animalClass), {
        method: "POST",
        headers: { [IDEMPOTENCY_HEADER]: intentKey.current },
        body,
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        // The backend returns EVERY problem from one pass, each naming its sheet row. Showing one
        // at a time would have an author uploading all afternoon.
        setProblems(
          Array.isArray(payload?.problems)
            ? payload.problems.map((p: string) => ({ message: p }))
            : [{ message: payload?.message ?? copy(pageContract, "action.error_backend") }],
        );
        return;
      }
      intentKey.current = "";
      intentFile.current = "";
      setNote(
        `${copy(pageContract, "action.sheet_imported")} — ${payload?.questions ?? 0} / ${payload?.rules ?? 0}`,
      );
      // Warnings a publish WOULD allow through, surfaced now rather than after publishing.
      if (Array.isArray(payload?.warnings)) {
        setProblems(payload.warnings.map((warning: string | Problem) =>
          typeof warning === "string" ? { message: warning } : warning,
        ));
      }
      onImported?.();
    } catch {
      setProblems([{ message: copy(pageContract, "action.error_backend") }]);
    } finally {
      setBusy(false);
      if (fileInput.current) fileInput.current.value = "";
    }
  }

  return (
    <Stack spacing={0.75}>
      <Stack direction="row" spacing={0.75} useFlexGap sx={{ alignItems: "center", flexWrap: "wrap" }}>
        <Button
          component="a"
          variant="outlined"
          color="inherit"
          size="small"
          href={href("export", "xlsx")}
          aria-label={`${copy(pageContract, "action.download_sheet")} — ${typeLabel}`}
          startIcon={<Iconify icon="solar:download-bold" aria-hidden="true" />}
        >
          {copy(pageContract, "action.download_sheet")}
        </Button>
        <Button
          component="a"
          variant="outlined"
          color="inherit"
          size="small"
          href={href("export", "json")}
          aria-label={`${copy(pageContract, "action.download_json")} — ${typeLabel}`}
          startIcon={<Iconify icon="solar:download-bold" aria-hidden="true" />}
        >
          {copy(pageContract, "action.download_json")}
        </Button>
        <Button
          type="button"
          variant="outlined"
          color="inherit"
          size="small"
          disabled={!mayWrite || busy}
          title={!mayWrite ? disabledReason : ""}
          onClick={() => fileInput.current?.click()}
          startIcon={<Iconify icon="solar:import-bold" aria-hidden="true" />}
        >
          {copy(pageContract, "action.upload_sheet")}
        </Button>
        <input type="file" ref={fileInput} accept=".csv,.xlsx,.json" hidden
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) void upload(f);
          }}
        />
      </Stack>
      {note ? (
        <Typography variant="body2" component="div" sx={{ color: "success.main" }}>{note}</Typography>
      ) : null}
      {problems.length > 0 ? (
        <Box
          component="ul"
          sx={{ typography: "body2", color: "text.secondary", mt: 0.25, mb: 0, ml: 2, pl: 0, maxWidth: 620 }}
        >
          {problems.slice(0, 12).map((p, i) => (
            <Box component="li" key={i} sx={p.fatal === false ? undefined : { color: "error.main" }}>
              {p.path ? `${p.path}: ` : ""}
              {p.message}
            </Box>
          ))}
          {problems.length > 12 ? (
            <li>{`+${problems.length - 12}`}</li>
          ) : null}
        </Box>
      ) : null}
    </Stack>
  );
}

/**
 * The sheet controls that belong ONCE, at the top of the section.
 *
 * THE BLANK TEMPLATE IS THE SAME FILE FOR EVERY TYPE -- the handler builds it from the column
 * header and one worked example of each row kind, and never reads the animal class -- so repeating
 * the button on every row was six ways to download one file, and made the row's own
 * type-specific "Download rules" harder to spot.
 *
 * The draft note moves here too, as the console's own "i". It is a standing fact about what an
 * upload does, not a per-row one: saying it six times made the table taller without making it
 * clearer, and a reader who has read it once does not need it again beside every category.
 */
export function RegisterSheetHeaderControls({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <Stack component="span" direction="row" spacing={1} sx={{ display: "inline-flex", alignItems: "center" }}>
      <Button
        component="a"
        variant="outlined"
        color="inherit"
        size="small"
        href={registerSheetHref(TEMPLATE_CLASS_PLACEHOLDER, "template", "xlsx")}
        startIcon={<Iconify icon="solar:download-bold" aria-hidden="true" />}
      >
        {copy(pageContract, "action.download_template")}
      </Button>
      {/* Opens LEFTWARD: this "i" sits at the right end of the card header, and `.card` clips
          its overflow, so a rightward panel was cut off mid-sentence. */}
      <InfoTip title={optionalCopy(pageContract, "note.sheet_writes_a_draft") ?? ""} />
    </Stack>
  );
}
