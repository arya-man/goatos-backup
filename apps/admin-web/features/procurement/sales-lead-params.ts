// The URL parameters each pipeline board reads its search, facet, page and reopened row from.
//
// Deliberately in a module of its OWN, with no "use client" directive: these constants are read by
// the Sales Config SERVER component to build its two lead reads, and by the CLIENT board to write
// the URL back. An export of a "use client" module reaches a server component as a client
// reference, not as the object -- every name resolves to undefined there, and the page silently
// serves an unfiltered first page while the address bar says otherwise. That defect is exactly what
// this file prevents.
//
// One set per panel rather than one shared set: both boards are open on the same URL, and a shared
// `search` would narrow the farmer groups the moment a buyer name was typed.

/** The four parameter names one board owns. */
export type LeadParamNames = { search: string; status: string; offset: string; open: string };

export const BUYER_LEAD_PARAMS: LeadParamNames = {
  search: "buyer_search",
  status: "buyer_status",
  offset: "buyer_offset",
  open: "buyer_open",
};

export const FPO_LEAD_PARAMS: LeadParamNames = {
  search: "group_search",
  status: "group_status",
  offset: "group_offset",
  open: "group_open",
};
