"use client";

import type { MouseEvent, ReactNode } from "react";

import Box from "@mui/material/Box";

import { CustomBreadcrumbs } from "@/components/minimal/custom-breadcrumbs";
import { BackLink } from "@/components/minimal/custom-breadcrumbs/back-link";
import { useNavTrail } from "@/components/shell/nav-trail-context";
import { iconifyClasses } from "@/components/minimal/iconify";

export type PageCrumb = { label: string; href?: string };

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
};

/**
 * THE page header: the template's `CustomBreadcrumbs` (heading, full-path links, actions on the
 * right), one per page. There is deliberately no description slot — a page explains itself with its
 * labels, fields, tables and buttons.
 */
export function PageHeader({ title, crumbs: crumbsIn, actions, backHref, tabs, toolbar, className, id }: PageHeaderProps) {
  const trail = useNavTrail();
  // A trail that only repeats the page title ("Approvals • Approvals") tells the reader nothing.
  const crumbs = crumbsIn && crumbsIn.length > 0 && !crumbsIn.every((crumb) => crumb.label.trim() === title.trim()) ? crumbsIn : [];
  const last = trail.items[trail.items.length - 1];
  const back = last ? last.href : backHref;

  return (
    <Box component="header" className={className} id={id} data-page-header="" sx={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--sp-3)",
        // The template hangs the back arrow into the page gutter; below md the page column clips
        // sideways overflow (WebView rule), so the arrow sits inline there instead of being cut off.
        [`& .minimal__breadcrumbs__back .${iconifyClasses.root}`]: { ml: { xs: 0, md: "-18px" } },
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
        action={actions ? <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1.5, alignItems: "center" }}>{actions}</Box> : undefined}
        slotProps={{
          heading: { as: "h1", className: "kit-page-title" } as never,
        }}
      />
      {tabs ? <div>{tabs}</div> : null}
      {toolbar ? <div>{toolbar}</div> : null}
    </Box>
  );
}
