"use client";

import Box from "@mui/material/Box";
import { useState } from "react";
import ClickAwayListener from "@mui/material/ClickAwayListener";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import { Iconify } from "@/components/minimal/iconify";

/**
 * A caveat about a nearby control as a template info tooltip (guard: info-tip-tap).
 *
 * MUI's touch path cannot open a Tooltip on a plain tap: with enterTouchDelay 0 the open timer is
 * still cleared by the touchend that arrives in the same task, so in the Android WebView nothing
 * shows. Here the tooltip is controlled: a click / tap opens it, hover and keyboard focus open it on
 * desktop, click-away / Escape / mouse leave close it. The 44px IconButton carries the text as its
 * accessible name.
 */
export function InfoTip({ title, testId }: { title: string; testId?: string }) {
  const [open, setOpen] = useState(false);
  return (
    <ClickAwayListener onClickAway={() => setOpen(false)}>
      <Box component="span" sx={{ display: "inline-flex", flexShrink: 0 }}>
        <Tooltip
          title={title}
          arrow
          open={open}
          onOpen={() => setOpen(true)}
          onClose={() => setOpen(false)}
          disableTouchListener
          slotProps={{ tooltip: { sx: { maxWidth: 300 } } }}
        >
          <IconButton
            aria-label={title}
            data-testid={testId}
            onClick={() => setOpen(true)}
            sx={{ width: "var(--tap-min)", height: "var(--tap-min)", color: "text.disabled" }}
          >
            <Iconify icon="eva:info-outline" width={20} />
          </IconButton>
        </Tooltip>
      </Box>
    </ClickAwayListener>
  );
}
