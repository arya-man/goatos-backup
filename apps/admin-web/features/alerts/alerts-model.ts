// Shared constants for the Alerts page. Plain module (no "use client") so the server page and the
// client drawer read the SAME string: a constant exported from a client module reaches a server
// component as a client reference, not a value.
export const ALERTS_PATH = "/alerts";
export const PARAM_CONFIGURE = "configure";
