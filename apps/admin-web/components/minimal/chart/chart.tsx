'use client';

import type { ChartProps } from './types';

import { lazy, useRef, useMemo, useState, useEffect, Suspense } from 'react';
import { mergeClasses } from 'minimal-shared/utils';

import NoSsr from '@mui/material/NoSsr';
import { styled, useColorScheme } from '@mui/material/styles';

import { chartClasses } from './classes';
import { ChartLoading } from './components';

// ----------------------------------------------------------------------

const LazyChart = lazy(() => import('react-apexcharts'));

export function Chart({
  sx,
  type,
  series,
  slotProps,
  className,
  options = {},
  deps,
  ...other
}: ChartProps & {
  /**
   * Mesha addition. Values the option formatters close over (display strings, tips, labels).
   * A change here always applies the new options; see `useStableOptions`.
   */
  deps?: ReadonlyArray<unknown>;
}) {
  const stableOptions = useStableOptions(options, series, deps);
  const { mode } = useColorScheme();
  const resolvedOptions = useMemo(() => resolveCssVars(stableOptions, mode), [stableOptions, mode]);
  // Per-point colours (`{ x, y, fillColor }`) live in the series, so they resolve the same way.
  const resolvedSeries = useMemo(() => resolveCssVars(series, mode), [series, mode]);

  const rootRef = useRef<HTMLDivElement | null>(null);
  useTooltipInView(rootRef);

  const renderFallback = () => <ChartLoading type={type} sx={slotProps?.loading} />;

  return (
    <ChartRoot
      ref={rootRef}
      dir="ltr"
      className={mergeClasses([chartClasses.root, className])}
      sx={sx}
      {...other}
    >
      <NoSsr fallback={renderFallback()}>
        <Suspense fallback={renderFallback()}>
          <LazyChart type={type} series={resolvedSeries} options={resolvedOptions} width="100%" height="100%" />
        </Suspense>
      </NoSsr>
    </ChartRoot>
  );
}

// ----------------------------------------------------------------------

/**
 * Mesha addition. react-apexcharts compares options by identity for functions, so a formatter
 * rebuilt on every render calls `updateOptions` on every parent render: the chart redraws and an
 * open tooltip flickers. Options that only differ by function identity (same source) keep the
 * previous object while the series is unchanged. A formatter's source text cannot show what it
 * closes over, so callers pass those values as `deps`: a changed dep always takes the new options.
 */
function useStableOptions(
  options: ChartProps['options'],
  series: ChartProps['series'],
  deps: ReadonlyArray<unknown> = []
) {
  const [kept, setKept] = useState({ options, series, deps });
  const sameDeps = kept.deps.length === deps.length && kept.deps.every((d, i) => Object.is(d, deps[i]) || sameShape(d, deps[i]));
  if (sameDeps && kept.options === options && kept.series === series) return kept.options;
  if (sameDeps && sameShape(kept.series, series) && sameShape(kept.options, options)) return kept.options;
  setKept({ options, series, deps });
  return options;
}

/**
 * Mesha addition. Apex keeps its tooltip inside the chart's own width, but a chart wider than its
 * card (a sideways-scrolling column strip on a phone) is clipped by the scroller and the screen.
 * The tooltip is shifted (CSS `translate`, which Apex never writes) to stay inside the nearest
 * clipping box and the viewport. Only the tooltip node is observed, once the reader touches it.
 */
function useTooltipInView(root: React.RefObject<HTMLDivElement | null>) {
  useEffect(() => {
    const el = root.current;
    if (!el || typeof MutationObserver === 'undefined') return undefined;
    const GAP = 4;
    // Measured from layout (offsetLeft/offsetWidth), which the translate never moves, so a shift
    // can never feed back into the next measurement.
    const fit = (tip: HTMLElement) => {
      const parent = tip.offsetParent as HTMLElement | null;
      if (!parent) return;
      const shift = Number(tip.dataset.shift || 0);
      const baseLeft = parent.getBoundingClientRect().left + parent.clientLeft + tip.offsetLeft;
      const baseRight = baseLeft + tip.offsetWidth;
      let left = 0;
      let right = window.innerWidth;
      for (let a = el.parentElement; a && a !== document.body; a = a.parentElement) {
        const o = getComputedStyle(a).overflowX;
        if (o === 'auto' || o === 'scroll' || o === 'hidden' || o === 'clip') {
          const b = a.getBoundingClientRect();
          left = Math.max(left, b.left);
          right = Math.min(right, b.right);
          break;
        }
      }
      let next = 0;
      if (baseRight > right - GAP) next = right - GAP - baseRight;
      if (baseLeft + next < left + GAP) next = left + GAP - baseLeft;
      next = Math.round(next);
      if (next !== shift) {
        tip.dataset.shift = String(next);
        tip.style.translate = next ? `${next}px 0` : '';
      }
    };
    const watched = new WeakSet<Element>();
    const mo = new MutationObserver((records) => records.forEach((m) => fit(m.target as HTMLElement)));
    const attach = () => {
      el.querySelectorAll<HTMLElement>('.apexcharts-tooltip').forEach((tip) => {
        if (watched.has(tip)) return;
        watched.add(tip);
        mo.observe(tip, { attributes: true, attributeFilter: ['style', 'class'] });
        tip.addEventListener('transitionend', () => fit(tip));
        fit(tip);
      });
    };
    el.addEventListener('pointerover', attach, { passive: true });
    el.addEventListener('touchstart', attach, { passive: true });
    return () => {
      mo.disconnect();
      el.removeEventListener('pointerover', attach);
      el.removeEventListener('touchstart', attach);
    };
  }, [root]);
}

/**
 * Mesha addition. ApexCharts does colour maths (gradients, hover shades, data-label contrast) on
 * the colour strings it is given, which a `var(--…)` defeats. Every `var(--x)` in the options is
 * resolved against the page for the active colour scheme, so series can use the Mesha tokens
 * (`var(--primary)`, `var(--teal)` …) and follow light/dark. Client only: the chart never SSRs.
 */
function resolveCssVars<T>(value: T, mode: unknown): T {
  if (typeof document === 'undefined') return value;
  const style = getComputedStyle(document.documentElement);
  const cache = new Map<string, string>();
  const read = (name: string) => {
    if (!cache.has(name)) cache.set(name, style.getPropertyValue(name).trim());
    return cache.get(name) as string;
  };
  // color-mix() is valid CSS but not a colour ApexCharts can parse: let the browser compute it.
  const mixed = (value: string) => {
    const probe = document.createElement('span');
    probe.style.color = value;
    probe.style.display = 'none';
    document.body.appendChild(probe);
    const computed = getComputedStyle(probe).color;
    probe.remove();
    // The computed form is color(srgb …); a 1x1 canvas pixel turns it into plain rgba bytes.
    const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
    if (!ctx || !computed) return computed || value;
    ctx.fillStyle = computed;
    ctx.fillRect(0, 0, 1, 1);
    const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
    return a === 255 ? `rgb(${r}, ${g}, ${b})` : `rgba(${r}, ${g}, ${b}, ${Math.round((a / 255) * 100) / 100})`;
  };
  const walk = (v: unknown): unknown => {
    if (typeof v === 'string') {
      const out = v.includes('var(--') ? v.replace(/var\((--[\w-]+)(?:,\s*([^)]+))?\)/g, (m, name: string, fallback?: string) => read(name) || fallback || m) : v;
      return out.includes('color-mix(') ? mixed(out) : out;
    }
    if (Array.isArray(v)) return v.map(walk);
    if (v && typeof v === 'object' && Object.getPrototypeOf(v) === Object.prototype) {
      return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, walk(x)]));
    }
    return v;
  };
  void mode;
  return walk(value) as T;
}

function sameShape(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a === 'function' && typeof b === 'function') return String(a) === String(b);
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  const ka = Object.keys(a);
  const kb = Object.keys(b);
  if (ka.length !== kb.length) return false;
  return ka.every((k) => sameShape((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]));
}

// ----------------------------------------------------------------------

const ChartRoot = styled('div')(({ theme }) => ({
  width: '100%',
  flexShrink: 0,
  position: 'relative',
  borderRadius: Number(theme.shape.borderRadius) * 1.5,
}));
