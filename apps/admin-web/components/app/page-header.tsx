"use client";

import type { MouseEvent, ReactNode } from "react";

import Box from "@mui/material/Box";

import { CustomBreadcrumbs } from "@/components/minimal/custom-breadcrumbs";
import { BackLink } from "@/components/minimal/custom-breadcrumbs/back-link";
import { useNavTrail } from "@/components/shell/nav-trail-context";
import { iconifyClasses } from "@/components/minimal/iconify";

export type PageCrumb = { label: string; href?: string };

/**
 * How the header lays out its actions, shared by `PageHeader` and its loading twin
 * `PageHeaderSkeleton` (TR3-P0-1). A route declares ONE constant of this type in its layout file and
 * passes it to both, so the skeleton cannot drift from the page (guard: page-header-layout-twin).
 */
export type PageHeaderLayout = {
  /**
   * The actions stay on the title row at every width (the title and crumbs shrink/wrap beside them)
   * instead of wrapping under the title block on a phone. For one primary action (plus an overflow ⋮).
   */
  actionsInline?: boolean;
  /**
   * Below md the actions always take their own right-aligned row under the title block, whatever the
   * title / crumb widths or the number of actions (a permission-gated extra action, a long module
   * crumb). For a header whose actions and crumbs vary per route (the shared SOP library).
   */
  actionsBelow?: boolean;
};

/** The CustomBreadcrumbs container/content sx for a layout; the same object feeds page and skeleton. */
export function pageHeaderLayoutSx(layout: PageHeaderLayout | undefined) {
  if (layout?.actionsBelow) {
    return {
      "& > div:first-of-type": { flexDirection: { xs: "column", md: "row" }, alignItems: { xs: "flex-end", md: "flex-start" } },
      "& > div:first-of-type > div:first-of-type": { alignSelf: { xs: "stretch", md: "auto" } },
    } as const;
  }
  if (!layout?.actionsInline) return undefined;
  return {
    "& > div:first-of-type": { flexWrap: "nowrap" },
    "& > div:first-of-type > div:first-of-type": { minWidth: 0 },
  } as const;
}

/** The actions Box sx for a layout (inline actions never shrink or wrap among themselves). */
export function pageHeaderActionsSx(layout: PageHeaderLayout | undefined) {
  return { display: "flex", flexWrap: layout?.actionsInline ? "nowrap" : "wrap", flexShrink: layout?.actionsInline ? 0 : undefined, gap: 1.5, alignItems: "center" } as const;
}

export type PageHeaderProps = {
  title: string;
  /** Full path, "Module • Page". The last crumb is the current page and renders disabled. */
  crumbs?: PageCrumb[];
  /** Primary action(s), right-aligned. */
  actions?: ReactNode;
  /**
   * Parent page for an L2/L3 route. The heading itself becomes the back link (template
   * `CustomBreadcrumbs` `backHref`). The session trail wins when the reader arrived from another page.
   */
  backHref?: string;
  /** One tab strip under the header. */
  tabs?: ReactNode;
  /** Filter row / scope controls under the tabs. */
  toolbar?: ReactNode;
  className?: string;
  id?: string;
  /** Shared with the route's PageHeaderSkeleton (a named constant, never a literal). */
  layout?: PageHeaderLayout;
};

/**
 * THE page header: the template's `CustomBreadcrumbs` (heading, full-path links, actions on the
 * right), one per page. There is deliberately no description slot — a page explains itself with its
 * labels, fields, tables and buttons.
 */
/** The back arrow beside a back-title heading: an 8px gap, hung into the gutter from md. */
export const BACK_ICON_SX = { mr: 1, ml: { xs: 0, md: "-26px" } } as const;

export function PageHeader({ title, crumbs: crumbsIn, actions, backHref, tabs, toolbar, className, id, layout }: PageHeaderProps) {
  const trail = useNavTrail();
  // A trail that only repeats the page title ("Approvals • Approvals") tells the reader nothing.
  const crumbs = crumbsIn && crumbsIn.length > 0 && !crumbsIn.every((crumb) => crumb.label.trim() === title.trim()) ? crumbsIn : [];
  const last = trail.items[trail.items.length - 1];
  const back = last ? last.href : backHref;

  return (
    <Box component="header" className={className} id={id} data-page-header="" sx={{
        display: "flex",
        flexDirection: "column",
        gap: 3,
        // The template hangs the back arrow into the page gutter; below md the page column clips
        // sideways overflow (WebView rule), so the arrow sits inline there instead of being cut off.
        // The arrow keeps an 8px gap to the title ("‹Channapatna" read glued, J2 P2-10) and still hangs
        // in the gutter at md, so the title stays on the column edge. Twin: PageHeaderSkeleton
        // (BACK_ICON_SX; guard back-arrow-gap in page-header-back.test.mjs).
        [`& .minimal__breadcrumbs__back .${iconifyClasses.root}`]: BACK_ICON_SX,
      }}>
      {/* The back arrow is the verbatim template BackLink rendered as the heading (CustomBreadcrumbs
          prints `heading` inside its h1); the in-app trail makes it a history back with its own title. */}
      <CustomBreadcrumbs
        heading={
          (back ? (
            <BackLink
              href={back}
              label={title}
              className="minimal__breadcrumbs__back"
              {...(last
                ? {
                    title: trail.backTitle(last),
                    "data-nav-back": "true",
                    onClick: (event: MouseEvent<HTMLAnchorElement>) => {
                      event.preventDefault();
                      trail.back();
                    },
                  }
                : {})}
            />
          ) : (
            title
          )) as unknown as string
        }
        links={crumbs.map((crumb) => ({ name: crumb.label, href: crumb.href }))}
        action={actions ? <Box sx={pageHeaderActionsSx(layout)}>{actions}</Box> : undefined}
        sx={pageHeaderLayoutSx(layout)}
        slotProps={{
          heading: { as: "h1", className: "kit-page-title" } as never,
        }}
      />
      {tabs ? <div>{tabs}</div> : null}
      {toolbar ? <div>{toolbar}</div> : null}
    </Box>
  );
}
