import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within, waitFor, screen } from "storybook/test";
import { usePopover } from "minimal-shared/hooks";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Divider from "@mui/material/Divider";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import FormControlLabel from "@mui/material/FormControlLabel";
import Checkbox from "@mui/material/Checkbox";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { ConfirmDialog } from "@/components/minimal/custom-dialog";
import { MinimalDrawer, DrawerSection } from "@/components/minimal/drawer";
import { withMinimalTheme, mobile } from "./_minimal";

function PopoverMenu({ initialOpen = false }: { initialOpen?: boolean }) {
  const pop = usePopover();
  const ref = React.useRef<HTMLButtonElement>(null);
  React.useEffect(() => {
    if (initialOpen && ref.current) pop.setAnchorEl(ref.current);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialOpen]);
  return (
    <Box sx={{ display: "flex", justifyContent: "flex-end", minHeight: 260 }}>
      <IconButton ref={ref} aria-label="Row actions" onClick={pop.onOpen} color={pop.open ? "inherit" : "default"}>
        <Iconify icon="eva:more-vertical-fill" />
      </IconButton>
      <CustomPopover open={pop.open} anchorEl={pop.anchorEl} onClose={pop.onClose} slotProps={{ arrow: { placement: "right-top" } }}>
        <MenuList>
          <MenuItem onClick={pop.onClose}><Iconify icon="solar:eye-bold" />View</MenuItem>
          <MenuItem onClick={pop.onClose}><Iconify icon="solar:pen-bold" />Edit</MenuItem>
          <Divider sx={{ borderStyle: "dashed" }} />
          <MenuItem onClick={pop.onClose} sx={{ color: "error.main" }}><Iconify icon="solar:trash-bin-trash-bold" />Delete</MenuItem>
        </MenuList>
      </CustomPopover>
    </Box>
  );
}

function ConfirmDemo({ initialOpen = false }: { initialOpen?: boolean }) {
  const [open, setOpen] = React.useState(initialOpen);
  return (
    <>
      <Button variant="outlined" color="error" onClick={() => setOpen(true)}>Delete goat</Button>
      <ConfirmDialog
        open={open}
        onClose={() => setOpen(false)}
        title="Delete"
        content="Are you sure you want to delete MSG-01040? This cannot be undone."
        action={<Button variant="contained" color="error" onClick={() => setOpen(false)}>Delete</Button>}
      />
    </>
  );
}

function FiltersDrawerDemo({ initialOpen = false }: { initialOpen?: boolean }) {
  const [open, setOpen] = React.useState(initialOpen);
  const [sick, setSick] = React.useState(true);
  return (
    <>
      <Button variant="outlined" color="inherit" startIcon={<Iconify icon="ic:round-filter-list" />} onClick={() => setOpen(true)}>Filters</Button>
      <MinimalDrawer open={open} onClose={() => setOpen(false)} title="Filters" onReset={() => setSick(false)} canReset={sick} invisibleBackdrop>
        <DrawerSection title="Status">
          <FormControlLabel control={<Checkbox checked={sick} onChange={(e) => setSick(e.target.checked)} />} label="Sick only" />
          <FormControlLabel control={<Checkbox />} label="Include sold" />
        </DrawerSection>
        <DrawerSection title="Pen">
          <TextField size="small" placeholder="Pen name" fullWidth />
        </DrawerSection>
      </MinimalDrawer>
    </>
  );
}

function DetailsDrawerDemo({ initialOpen = false }: { initialOpen?: boolean }) {
  const [open, setOpen] = React.useState(initialOpen);
  return (
    <>
      <Button variant="contained" onClick={() => setOpen(true)}>Open details</Button>
      <MinimalDrawer
        open={open}
        onClose={() => setOpen(false)}
        title="MSG-01040"
        width={480}
        footer={<><Button variant="outlined" color="inherit" onClick={() => setOpen(false)}>Cancel</Button><Button variant="contained" onClick={() => setOpen(false)}>Save</Button></>}
      >
        <Box sx={{ p: 2.5, display: "grid", gap: 2 }}>
          {Array.from({ length: 14 }, (_, i) => (
            <Typography key={i} variant="body2">Detail line {i + 1}: long-form drawer content scrolls inside the drawer body.</Typography>
          ))}
        </Box>
      </MinimalDrawer>
    </>
  );
}

const meta: Meta = { title: "Minimal/Overlays", decorators: [withMinimalTheme] };
export default meta;
type Story = StoryObj;

export const PopoverClosed: Story = {
  render: () => <PopoverMenu />,
  play: async ({ canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Row actions" }));
    await waitFor(() => expect(screen.getByRole("menuitem", { name: /Edit/ })).toBeVisible());
    await userEvent.click(screen.getByRole("menuitem", { name: /Edit/ }));
  },
};
export const PopoverOpen: Story = { render: () => <PopoverMenu initialOpen /> };
export const Confirm: Story = { render: () => <ConfirmDemo initialOpen /> };
export const FiltersDrawer: Story = { render: () => <FiltersDrawerDemo initialOpen /> };
export const DetailsDrawer: Story = { render: () => <DetailsDrawerDemo initialOpen /> };
export const PopoverOpenMobile: Story = { render: () => <PopoverMenu initialOpen />, globals: mobile };
export const ConfirmMobile: Story = { render: () => <ConfirmDemo initialOpen />, globals: mobile };
export const FiltersDrawerMobile: Story = { render: () => <FiltersDrawerDemo initialOpen />, globals: mobile };
export const DetailsDrawerMobile: Story = { render: () => <DetailsDrawerDemo initialOpen />, globals: mobile };
