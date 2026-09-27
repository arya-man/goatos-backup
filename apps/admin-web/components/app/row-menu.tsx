"use client";

import type { ReactNode } from "react";
import { usePopover } from "minimal-shared/hooks";
import IconButton from "@mui/material/IconButton";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { Iconify } from "@/components/minimal/iconify";
import { TAP_MIN, phoneTapSx } from "@/components/app/tap";

export type RowMenuAction = {
  label: ReactNode;
  onSelect: () => void;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
};

export type RowMenuProps = {
  actions: RowMenuAction[];
  ariaLabel?: string;
  /** Which edge of the trigger the menu hangs from; default "right" (menu opens to the left). */
  align?: "left" | "right";
  className?: string;
  /** Custom trigger content; defaults to the template's vertical-dots icon. */
  children?: ReactNode;
};

/**
 * The ⋮ overflow menu at the end of a table or list row — the template's table-row action pattern
 * (sections/user/user-table-row.tsx): IconButton + usePopover + CustomPopover + MenuList.
 *
 * Product behaviour kept here so no call site re-implements it: an actions array (disabled
 * actions are dropped, danger actions wear error.main) and the menu closes before the action runs.
 * MUI Popover gives the portal, viewport-clamped placement, Escape / outside-click dismissal and
 * focus return to the trigger. `kit-rowmenu-btn` stays on the trigger as the hook the phone table
 * rules use to pin and size the kebab column.
 */
export function RowMenu({ actions, ariaLabel = "Row actions", align = "right", className, children }: RowMenuProps) {
  const menu = usePopover();
  return (
    <>
      <IconButton
        className={className ? `kit-rowmenu-btn ${className}` : "kit-rowmenu-btn"}
        aria-label={ariaLabel}
        aria-haspopup="menu"
        aria-expanded={menu.open}
        color={menu.open ? "inherit" : "default"}
        onClick={menu.onOpen}
        sx={phoneTapSx}
      >
        {children ?? <Iconify icon="eva:more-vertical-fill" />}
      </IconButton>
      <CustomPopover
        open={menu.open}
        anchorEl={menu.anchorEl}
        onClose={menu.onClose}
        slotProps={{ arrow: { placement: align === "right" ? "right-top" : "left-top" } }}
      >
        <MenuList aria-label={ariaLabel}>
          {actions.map((a, i) =>
            a.disabled ? null : (
              <MenuItem
                key={i}
                onClick={() => {
                  menu.onClose();
                  a.onSelect();
                }}
                sx={(theme) => ({
                  [theme.breakpoints.down("sm")]: { minHeight: TAP_MIN },
                  ...(a.danger ? { color: theme.palette.error.main } : null),
                })}
              >
                {a.icon}
                {a.label}
              </MenuItem>
            ),
          )}
        </MenuList>
      </CustomPopover>
    </>
  );
}
