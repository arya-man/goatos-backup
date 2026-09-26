"use client";

import { usePopover } from "minimal-shared/hooks";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Tooltip from "@mui/material/Tooltip";
import MenuList from "@mui/material/MenuList";
import MenuItem from "@mui/material/MenuItem";
import IconButton from "@mui/material/IconButton";
import ToggleButton from "@mui/material/ToggleButton";
import LinearProgress from "@mui/material/LinearProgress";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";

import { Iconify } from "@/components/minimal/iconify";
import type { IconifyName } from "@/layouts/template/iconify";
import { CustomPopover } from "@/components/minimal/custom-popover";

// CalendarToolbar — copied from Minimal_TypeScript_v7.7.0 next-ts
// src/sections/calendar/calendar-toolbar.tsx and adapted for our read-only
// /calendar (no "Add event" button, no filters drawer button — those template
// affordances belong to the editable demo). Prev / Today / Next, the date
// title, and the view switcher stay unchanged.
export type CalendarView = "dayGridMonth" | "timeGridWeek" | "timeGridDay" | "listWeek";

type CalendarToolbarProps = {
  view: CalendarView;
  title: string;
  loading?: boolean;
  onChangeView: (view: CalendarView) => void;
  onDateNavigation: (action: "today" | "prev" | "next") => void;
  viewOptions: { label: string; value: CalendarView; icon: IconifyName }[];
  // Copy passed in by the caller so this kit toolbar carries no local literals.
  todayLabel: string;
  previousLabel: string;
  nextLabel: string;
  viewGroupAriaLabel: string;
};

export function CalendarToolbar({
  view,
  title,
  loading,
  onChangeView,
  onDateNavigation,
  viewOptions,
  todayLabel,
  previousLabel,
  nextLabel,
  viewGroupAriaLabel,
}: CalendarToolbarProps) {
  const mobileActions = usePopover();

  const selectedView = viewOptions.find((option) => option.value === view) ?? viewOptions[0];

  const renderDesktopMenuItems = () => (
    <ToggleButtonGroup
      exclusive
      size="small"
      aria-label={viewGroupAriaLabel}
      value={view}
      onChange={(_event, newAlignment: CalendarView | null) => {
        if (newAlignment !== null) {
          onChangeView(newAlignment);
        }
      }}
      sx={{ display: { xs: "none", sm: "inline-flex" } }}
    >
      {viewOptions.map((option) => (
        <Tooltip key={option.value} title={option.label}>
          <ToggleButton value={option.value} aria-label={`${option.label} view`}>
            <Iconify icon={option.icon} />
          </ToggleButton>
        </Tooltip>
      ))}
    </ToggleButtonGroup>
  );

  const renderMobileMenuItems = () => (
    <>
      <Button
        size="small"
        color="inherit"
        onClick={mobileActions.onOpen}
        sx={{ minWidth: "auto", display: { sm: "none" } }}
      >
        <Iconify icon={selectedView.icon} sx={{ mr: 0.5 }} />
        <Iconify icon="eva:arrow-ios-downward-fill" width={18} />
      </Button>

      <CustomPopover
        open={mobileActions.open}
        anchorEl={mobileActions.anchorEl}
        onClose={mobileActions.onClose}
        slotProps={{ arrow: { placement: "top-left" } }}
      >
        <MenuList>
          {viewOptions.map((option) => (
            <MenuItem
              key={option.value}
              selected={option.value === view}
              onClick={() => {
                mobileActions.onClose();
                onChangeView(option.value);
              }}
            >
              <Iconify icon={option.icon} />
              {option.label}
            </MenuItem>
          ))}
        </MenuList>
      </CustomPopover>
    </>
  );

  const renderDateNavigation = () => (
    <Box
      sx={{
        gap: { sm: 1 },
        display: "flex",
        flex: "1 1 auto",
        textAlign: "center",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <IconButton onClick={() => onDateNavigation("prev")} aria-label={previousLabel}>
        <Iconify icon="eva:arrow-ios-back-fill" />
      </IconButton>

      <Box sx={{ typography: { xs: "subtitle2", sm: "h6" } }}>{title}</Box>

      <IconButton onClick={() => onDateNavigation("next")} aria-label={nextLabel}>
        <Iconify icon="eva:arrow-ios-forward-fill" />
      </IconButton>
    </Box>
  );

  const renderToday = () => (
    <Box sx={{ gap: 1, display: "flex", alignItems: "center" }}>
      <Button
        size="small"
        color="inherit"
        variant="outlined"
        onClick={() => onDateNavigation("today")}
      >
        {todayLabel}
      </Button>
    </Box>
  );

  const renderLoading = () => (
    <LinearProgress
      color="inherit"
      sx={{
        left: 0,
        width: 1,
        height: 2,
        bottom: 0,
        borderRadius: 0,
        position: "absolute",
      }}
    />
  );

  return (
    <Box
      sx={{ pr: 2, pl: 2.5, py: 2.5, display: "flex", alignItems: "center", position: "relative" }}
    >
      {renderDesktopMenuItems()}
      {renderMobileMenuItems()}
      {renderDateNavigation()}
      {renderToday()}
      {loading && renderLoading()}
    </Box>
  );
}
