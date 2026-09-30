"use client";

import { useState } from "react";

import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Button from "@mui/material/Button";
import CardHeader from "@mui/material/CardHeader";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";

import { SegmentTabs } from "@/components/app/list/segment-tabs";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { MailNavItem } from "@/components/app/sections/mail/mail-nav-item";
import { TAP_MIN } from "@/theme/tap-target";

/**
 * The Lists panel of Items & categories (the prototype's middle column): "All items", the
 * built-in catalogues (locked: they can be renamed, never removed) and the farm's own lists,
 * each with the number of items it holds. Choosing a list is a real navigation (the items table
 * is filtered server-side, whole subtree); the search box and the Active / Archived toggle
 * only narrow the list of NAMES already on screen, which is why they are local state.
 *
 * Template anatomy: a Card with CardHeader + count Label, the template search TextField, and the
 * mail nav rail (`MailNavItem`) for the lists, with a trailing edit IconButton per list.
 *
 * Renders no copy of its own: every label arrives resolved from the page contract, and every
 * href is precomputed by the server component.
 */

export type CatalogueList = {
  id: string;
  name: string;
  depth: number;
  /** Under CATALOGUES (its root is built in) rather than YOUR LISTS. */
  builtin: boolean;
  /** The row itself is built in: shown with the lock. */
  locked: boolean;
  archived: boolean;
  count: number;
  href: string;
  editHref: string;
};

function Subheader({ children }: { children: React.ReactNode }) {
  return (
    <Typography component="li" variant="overline" sx={{ display: "flex", alignItems: "center", gap: 0.75, px: 1, pt: 2, pb: 1, color: "text.disabled" }}>
      {children}
    </Typography>
  );
}

export function CatalogueLists({
  copy,
  lists,
  allHref,
  allCount,
  current,
  canEdit,
  newHref,
}: {
  copy: Record<string, string>;
  lists: CatalogueList[];
  allHref: string;
  allCount: number;
  /** The selected list id, "" for All items. */
  current: string;
  canEdit: boolean;
  newHref: string;
}) {
  const [q, setQ] = useState("");
  const [status, setStatus] = useState<"active" | "archived">("active");
  const needle = q.trim().toLowerCase();
  const shown = lists.filter((list) => (status === "archived" ? list.archived : !list.archived) && (!needle || list.name.toLowerCase().includes(needle)));
  const catalogues = shown.filter((list) => list.builtin);
  const yours = shown.filter((list) => !list.builtin);
  const row = (list: CatalogueList) => (
    <MailNavItem
      key={list.id}
      selected={list.id === current}
      href={list.href}
      slotProps={{ item: { alignItems: "center", gap: 0.5, pl: list.depth * 2 }, button: { minHeight: TAP_MIN }, label: { textTransform: "none" } }}
      label={{ name: list.name, count: list.count, icon: list.locked ? "solar:lock-password-outline" : undefined }}
      action={
        canEdit ? (
          <IconButton component={LocalOverlayLink} href={list.editHref} scroll={false} size="small" aria-label={`${copy["action.edit_row.label"]} ${list.name}`}>
            <Iconify icon="solar:pen-bold" width={18} />
          </IconButton>
        ) : null
      }
    />
  );
  return (
    <Card component="aside" aria-label={copy["lists.title"]}>
      <CardHeader
        title={
          <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
            {copy["lists.title"]}
            <Label variant="soft">{lists.filter((l) => !l.archived).length}</Label>
          </Box>
        }
        sx={{ pb: 2 }}
      />
      <Box sx={{ px: 2, display: "flex", flexDirection: "column", gap: 1.5 }}>
        <TextField
          size="small"
          fullWidth
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={copy["lists.search"]}
          slotProps={{
            htmlInput: { "aria-label": copy["lists.search"] },
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
                </InputAdornment>
              ),
            },
          }}
        />
        <SegmentTabs
          ariaLabel={copy["column.status"]}
          value={status}
          tabs={[
            { value: "active", label: copy["status.active"], onClick: () => setStatus("active") },
            { value: "archived", label: copy["status.archived"], onClick: () => setStatus("archived") },
          ]}
        />
      </Box>
      <Box component="nav">
        <Box component="ul" sx={{ m: 0, p: 0, pb: 1.5, px: 1.5, listStyle: "none" }}>
          {status === "active" && !needle ? (
            <MailNavItem selected={current === ""} href={allHref} label={{ name: copy["lists.all"], count: allCount, icon: "solar:list-bold" }} slotProps={{ button: { minHeight: TAP_MIN }, label: { textTransform: "none" } }} />
          ) : null}
          {catalogues.length ? (
            <>
              <Subheader>
                <Iconify icon="solar:lock-password-outline" width={14} /> {copy["lists.catalogues"]}
              </Subheader>
              {catalogues.map(row)}
            </>
          ) : null}
          <Subheader>{copy["lists.yours"]}</Subheader>
          {yours.map(row)}
        </Box>
        {canEdit ? (
          <Box sx={{ px: 2, pb: 2 }}>
            <Button component={LocalOverlayLink} href={newHref} scroll={false} size="small" color="primary" startIcon={<Iconify icon="mingcute:add-line" />}>
              {copy["lists.new"]}
            </Button>
          </Box>
        ) : null}
      </Box>
    </Card>
  );
}
