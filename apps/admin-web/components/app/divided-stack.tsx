"use client";

import type { StackProps } from "@mui/material/Stack";

import Divider from "@mui/material/Divider";
import Stack from "@mui/material/Stack";

/**
 * MUI Stack with the template's dashed divider between children, built on the CLIENT.
 *
 * A server module must not hand Stack `divider={<Divider />}`: a JSX element in a non-children prop
 * crosses the RSC boundary as a lazy reference while the Divider chunk is still loading, Stack
 * `cloneElement`s it, and React throws "Element type is invalid ... got: undefined" -> the page's
 * "Something went wrong" (FJ1 P0-1: /goats/[goat_id] crashed on about half the loads, from the
 * streamed vaccination passport strip). Guard: server-element-prop.
 *
 * `dividerOrientation` follows the Stack direction you would have given the Divider; `solid` drops
 * the dashed style.
 */
export function DividedStack({
  dividerOrientation = "horizontal",
  solid = false,
  flexItem = true,
  ...other
}: Omit<StackProps, "divider"> & { dividerOrientation?: "horizontal" | "vertical"; solid?: boolean; flexItem?: boolean }) {
  return <Stack divider={<Divider flexItem={flexItem} orientation={dividerOrientation} sx={solid ? undefined : { borderStyle: "dashed" }} />} {...other} />;
}
