"use client";

// Portals children to <body> so a `position: fixed` drawer/dialog/backdrop anchors to the
// viewport rather than a transformed ancestor. Uses template MUI Portal (default container is
// document.body; SSR-safe: renders null until mount).
export { default as BodyPortal } from "@mui/material/Portal";
