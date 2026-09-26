'use client';

// The template's TablePaginationCustom (components/table/table-pagination-custom.tsx) for pagers whose
// state lives in the URL: MUI TablePagination with the template's footer layout, prev/next as links
// (TablePaginationActions anatomy: two IconButtons) and rows-per-page navigating to a prepared href.
// Everything is serializable, so a server page can render it with hrefs it computed.
import type { Theme, SxProps } from '@mui/material/styles';

import { useTransition } from 'react';
import { useRouter } from 'next/navigation';

import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import CircularProgress from '@mui/material/CircularProgress';
import TablePagination from '@mui/material/TablePagination';

import Link, { useLinkStatus } from '@/components/no-prefetch-link';

import { Iconify } from '../iconify';

export type TablePaginationLinksProps = {
  /** 0-based page shown. */
  page: number;
  rowsPerPage: number;
  /** Whole-result count; -1 when the backend pages by cursor and never counts. */
  count: number;
  /** Rows-per-page choice -> href. Omit to hide the control. */
  rowsPerPageHrefs?: { value: number; href: string }[];
  prevHref?: string | null;
  nextHref?: string | null;
  /** Client pagers only: runs on a plain click (may preventDefault to navigate itself). Enables the
   * control without an href (e.g. Previous = history back on a cursor list). */
  onPrevClick?: (event: React.MouseEvent<HTMLElement>) => void;
  onNextClick?: (event: React.MouseEvent<HTMLElement>) => void;
  /** Force a direction off even when an href/handler exists. */
  prevDisabled?: boolean;
  nextDisabled?: boolean;
  /** A readout-only footer (no pages to walk): range text, no arrows. */
  hideActions?: boolean;
  /** MUI TablePagination showFirstButton / showLastButton, as links (last may be disabled with a reason). */
  first?: { href: string | null; label: string };
  last?: { href: string | null; label: string; disabledReason?: string };
  labelRowsPerPage?: string;
  /** The range readout, already formatted by the page ("1–25 of 104 pens · Page 1"). */
  rangeLabel: string;
  prevLabel: string;
  nextLabel: string;
  /** Leading slot (the template's Dense switch position). */
  left?: React.ReactNode;
  ariaLabel?: string;
  /** Replace the history entry instead of pushing one (pagers that must not grow Back history). */
  replace?: boolean;
  className?: string;
  sx?: SxProps<Theme>;
};

export function TablePaginationLinks({
  page,
  rowsPerPage,
  count,
  rowsPerPageHrefs,
  prevHref,
  nextHref,
  onPrevClick,
  onNextClick,
  prevDisabled = false,
  nextDisabled = false,
  hideActions = false,
  first,
  last,
  labelRowsPerPage,
  rangeLabel,
  prevLabel,
  nextLabel,
  left,
  ariaLabel,
  replace = false,
  className,
  sx,
}: TablePaginationLinksProps) {
  const router = useRouter();
  const [, startTransition] = useTransition();
  const options = rowsPerPageHrefs?.map((option) => option.value) ?? [];

  return (
    <Box
      className={className}
      role={ariaLabel ? 'navigation' : undefined}
      aria-label={ariaLabel}
      sx={[{ position: 'relative' }, ...(Array.isArray(sx) ? sx : [sx])]}
    >
      <TablePagination
        component="div"
        count={count}
        page={page}
        rowsPerPage={rowsPerPage}
        rowsPerPageOptions={options.length ? options : [rowsPerPage]}
        onPageChange={() => undefined}
        onRowsPerPageChange={(event) => {
          const href = rowsPerPageHrefs?.find((option) => String(option.value) === event.target.value)?.href;
          if (href) startTransition(() => (replace ? router.replace(href, { scroll: false }) : router.push(href, { scroll: false })));
        }}
        labelRowsPerPage={labelRowsPerPage}
        labelDisplayedRows={() => rangeLabel}
        slotProps={{ select: { inputProps: { 'aria-label': labelRowsPerPage } } }}
        ActionsComponent={() => hideActions ? null : (
          <Box sx={{ flexShrink: 0, ml: 2.5, display: 'flex' }}>
            {first ? (
              <IconButton {...(first.href ? { component: Link, href: first.href, scroll: false, replace } : { disabled: true })} aria-label={first.label} title={first.label}>
                <Iconify icon="eva:arrowhead-left-fill" />
              </IconButton>
            ) : null}
            <IconButton
              {...(prevDisabled || (!prevHref && !onPrevClick) ? { disabled: true } : prevHref ? { component: Link, href: prevHref, scroll: false, replace } : {})}
              onClick={prevDisabled ? undefined : onPrevClick}
              aria-label={prevLabel}
              title={prevLabel}
            >
              <PendingArrow icon="eva:arrow-ios-back-fill" />
            </IconButton>
            <IconButton
              {...(nextDisabled || (!nextHref && !onNextClick) ? { disabled: true } : nextHref ? { component: Link, href: nextHref, scroll: false, replace } : {})}
              onClick={nextDisabled ? undefined : onNextClick}
              aria-label={nextLabel}
              title={nextLabel}
            >
              <PendingArrow icon="eva:arrow-ios-forward-fill" />
            </IconButton>
            {last ? (
              // A disabled button swallows its title, so the reason rides on a wrapping span.
              <Box component="span" title={last.href ? last.label : last.disabledReason ?? last.label}>
                <IconButton {...(last.href ? { component: Link, href: last.href, scroll: false, replace } : { disabled: true })} aria-label={last.label}>
                  <Iconify icon="eva:arrowhead-right-fill" />
                </IconButton>
              </Box>
            ) : null}
          </Box>
        )}
        sx={[
          { borderTopColor: 'transparent' },
          // Phone: the toolbar wraps so the arrows never slide out of the card, and grows with the
          // wrapped row instead of the theme's fixed 64px (which made the arrows a nested scroller).
          {
            overflow: { xs: 'visible', sm: 'auto' },
            '& .MuiTablePagination-toolbar': { flexWrap: { xs: 'wrap', sm: 'nowrap' }, justifyContent: 'flex-end', rowGap: 0.5, height: { xs: 'auto', sm: 64 }, minHeight: 64 },
          },
          // No rows-per-page choice on this pager: the template shows none rather than a one-item select.
          ...(rowsPerPageHrefs?.length ? [] : [{ '& .MuiTablePagination-selectLabel, & .MuiTablePagination-input': { display: 'none' } }]),
        ]}
      />
      {left ? (
        <Box sx={{ pl: 2, py: 1.5, top: 0, position: { sm: 'absolute' }, display: 'flex', alignItems: 'center' }}>{left}</Box>
      ) : null}
    </Box>
  );
}

/**
 * A pager arrow that says it is busy in place (main flicker fix, 2026-09-25): server-paged tables
 * keep the old page on screen until the next one is ready, so the pressed arrow turns into a small
 * spinner while its link is pending. Outside a link (a disabled or click-driven arrow) it is just the
 * arrow.
 */
function PendingArrow({ icon }: { icon: "eva:arrow-ios-back-fill" | "eva:arrow-ios-forward-fill" }) {
  const { pending } = useLinkStatus();
  return pending ? <CircularProgress size={16} color="inherit" aria-hidden /> : <Iconify icon={icon} />;
}
