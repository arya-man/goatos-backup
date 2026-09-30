"use client";

import { forwardRef, type ReactNode } from "react";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import type { SxProps, Theme } from "@mui/material/styles";

import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { TAP_MIN } from "@/components/app/tap";

/**
 * The field anatomy of the /tasks New task and Edit task dialogs, in the template form layout
 * (sections/product/product-new-edit-form: a `subtitle2` label over its control, fields stacked
 * with a 24px gap). Used for the fields that are not a labelled TextField: "For" (the shared
 * single-person picker), "Attachments" (the three pickers) and the kept-files list.
 */
export const TaskField = forwardRef<HTMLDivElement, { label: string; children: ReactNode; sx?: SxProps<Theme> }>(
  function TaskField({ label, children, sx }, ref) {
    return (
      <Box ref={ref} sx={[{ display: "grid", gap: 1 }, ...(Array.isArray(sx) ? sx : sx ? [sx] : [])]}>
        <Typography variant="subtitle2" component="span">
          {label}
        </Typography>
        {children}
      </Box>
    );
  },
);

export type TaskAttachmentPicker = { key: string; label: string; accept: string; icon: IconifyName };

/**
 * Voice note / Photo or video / File: three outlined template Buttons, each a `label` over a
 * hidden `<input type="file" name="attachment_file">` (the template upload-button pattern), so the
 * form posts the picked files exactly as before. The count of files picked per kind is a soft
 * Label inside the button. Three across from sm; on a phone two across with the last one full
 * width, every button 44px tall or more.
 */
export function TaskAttachmentPickers({
  pickers,
  counts,
  onPicked,
}: {
  pickers: readonly TaskAttachmentPicker[];
  counts: Record<string, number>;
  onPicked: (key: string, count: number) => void;
}) {
  return (
    <Box
      sx={{
        display: "grid",
        gap: 1,
        gridTemplateColumns: { xs: "repeat(2, minmax(0, 1fr))", sm: "repeat(3, minmax(0, 1fr))" },
        "& > :last-of-type": { gridColumn: { xs: "1 / -1", sm: "auto" } },
      }}
    >
      {pickers.map((picker) => {
        const count = counts[picker.key] ?? 0;
        return (
          <Button
            key={picker.key}
            component="label"
            variant="outlined"
            color="inherit"
            startIcon={<Iconify icon={picker.icon} aria-hidden="true" />}
            sx={{ minHeight: TAP_MIN, minWidth: 0 }}
          >
            <Box component="span" sx={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
              {picker.label}
            </Box>
            {count ? (
              <Label color="primary" sx={{ ml: 1, flex: "none" }}>
                {count}
              </Label>
            ) : null}
            <input type="file" hidden
              name="attachment_file"
              multiple
              accept={picker.accept}
              onChange={(event) => onPicked(picker.key, event.target.files?.length ?? 0)}
            />
          </Button>
        );
      })}
    </Box>
  );
}
