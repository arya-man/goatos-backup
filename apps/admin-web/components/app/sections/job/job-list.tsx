'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/job/job-list.tsx. Anatomy guarded; the demo jobs become the `children` SLOT (the
// page's own cards), `columns` is a declared override of the grid, and the centred MUI Pagination is
// URL-driven (every page item a Next link to a prepared href; declared renderItem/page/aria-label).
// A list already whole on the client passes `onSelect`: the items stay plain links to the same hrefs
// (deep links, open in a new tab) but a plain click only calls onSelect, with no server navigation.
import type { ReactNode, MouseEvent } from 'react';
import type { ResponsiveStyleValue } from '@mui/system';

import Box from '@mui/material/Box';
import PaginationItem from '@mui/material/PaginationItem';
import Pagination, { paginationClasses } from '@mui/material/Pagination';

import Link from '@/components/no-prefetch-link';

// ----------------------------------------------------------------------

export type JobListPagination = {
  /** 1-based page shown. */
  page: number;
  /** One href per page, in order; `null` for a page with no address. */
  hrefs: (string | null)[];
  ariaLabel?: string;
  /** Client-side paging: a plain click calls this instead of navigating (the caller updates the URL
   *  with history.replaceState). Modifier / middle clicks keep the link's own behaviour. */
  onSelect?: (page: number) => void;
};

type Props = {
  children: ReactNode;
  columns?: ResponsiveStyleValue<string>;
  pagination?: JobListPagination;
};

export function JobList({ children, columns, pagination }: Props) {
  // Client-side paging: a plain primary click selects the page in place; modifier / middle clicks
  // keep the plain link (new tab, copy link).
  const selectPage = (page: number | null) => (event: MouseEvent<HTMLAnchorElement>) => {
    if (!pagination?.onSelect || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    if (page) pagination.onSelect(page);
  };

  return (
    <>
      <Box
        sx={{
          gap: 3,
          display: 'grid',
          gridTemplateColumns: columns ?? { xs: 'repeat(1, 1fr)', sm: 'repeat(2, 1fr)', md: 'repeat(3, 1fr)' },
        }}
      >
        {children}
      </Box>

      {pagination && pagination.hrefs.length > 1 && (
        <Pagination
          count={pagination.hrefs.length}
          page={pagination.page}
          aria-label={pagination.ariaLabel}
          renderItem={(item) => {
            const href = item.page ? pagination.hrefs[item.page - 1] : null;
            return href && !item.disabled && item.type !== 'start-ellipsis' && item.type !== 'end-ellipsis' ? (
              pagination.onSelect ? (
                <PaginationItem
                  component="a"
                  href={href}
                  {...item}
                  onClick={selectPage(item.page)}
                />
              ) : (
                <PaginationItem component={Link} href={href} scroll={false} {...item} />
              )
            ) : (
              <PaginationItem {...item} disabled={item.disabled || !href} />
            );
          }}
          sx={{
            mt: { xs: 8, md: 8 },
            [`& .${paginationClasses.ul}`]: { justifyContent: 'center' },
          }}
        />
      )}
    </>
  );
}
