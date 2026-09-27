'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/job/job-list.tsx. Anatomy guarded; the demo jobs become the `children` SLOT (the
// page's own cards), `columns` is a declared override of the grid, and the centred MUI Pagination is
// URL-driven (every page item a Next link to a prepared href; declared renderItem/page/aria-label).
import type { ReactNode } from 'react';
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
};

type Props = {
  children: ReactNode;
  columns?: ResponsiveStyleValue<string>;
  pagination?: JobListPagination;
};

export function JobList({ children, columns, pagination }: Props) {
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
              <PaginationItem component={Link} href={href} scroll={false} {...item} />
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
