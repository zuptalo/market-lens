import { mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('lightweight-charts', () => import('./__mocks__/lightweight-charts'));

import PriceChart from './PriceChart.vue';
import {
  __dataOf,
  __lastChart,
  __reset,
  __series,
} from './__mocks__/lightweight-charts';
import {
  MISSING_SESSIONS,
  buildAction,
  buildBars,
  buildFinding,
} from '@/services/__fixtures__/marketData';

function mountChart(overrides: Record<string, unknown> = {}) {
  return mount(PriceChart, {
    attachTo: document.body,
    props: {
      bars: buildBars(),
      missingSessions: [...MISSING_SESSIONS],
      overlays: [],
      actions: [],
      findings: [],
      ...overrides,
    },
  });
}

describe('PriceChart', () => {
  beforeEach(() => __reset());

  it('draws exactly the stored observations and invents none', () => {
    const bars = buildBars();
    mountChart({ bars });
    const drawn = __dataOf('Candlestick') as Array<{ time: string; close?: number }>;
    const withValues = drawn.filter((point) => point.close !== undefined);
    expect(withValues).toHaveLength(bars.length);
    expect(withValues.map((point) => point.time)).toEqual(bars.map((bar) => bar.sessionDate));
  });

  it('interrupts the series at a missing session rather than bridging it', () => {
    mountChart();
    const drawn = __dataOf('Candlestick') as Array<{ time: string; close?: number }>;
    // A whitespace point at each absent session is what makes the chart show a break. Without
    // it the library joins the sessions either side and the gap disappears (FR-013).
    for (const missing of MISSING_SESSIONS) {
      const point = drawn.find((entry) => entry.time === missing);
      expect(point, `no point marks the missing session ${missing}`).toBeDefined();
      expect(point!.close).toBeUndefined();
    }
  });

  it('shows volume as its own aligned series and never draws a gap as zero', () => {
    mountChart();
    const drawn = __dataOf('Histogram') as Array<{ time: string; value?: number }>;
    for (const missing of MISSING_SESSIONS) {
      const point = drawn.find((entry) => entry.time === missing);
      expect(point?.value).toBeUndefined();
    }
  });

  it('keeps the TradingView attribution the licence requires', () => {
    mountChart();
    const options = __lastChart().options as { layout?: { attributionLogo?: boolean } };
    // Apache-2.0 plus an attribution requirement: the library renders the required link
    // itself, and turning this off to tidy the chart up would breach the licence.
    expect(options.layout?.attributionLogo).toBe(true);
  });

  it('draws an overlay only where it is defined', () => {
    mountChart({ overlays: [3] });
    const line = __dataOf('Line') as Array<{ time: string; value?: number }>;
    expect(line.length).toBeGreaterThan(0);
    const defined = line.filter((point) => point.value !== undefined);
    // The first two sessions cannot have a three-session average behind them.
    expect(defined[0].time).not.toBe(buildBars()[0].sessionDate);
  });

  it('marks a corporate action at its ex-date', () => {
    mountChart({ actions: [buildAction({ exDate: '2026-05-28' })] });
    const markers = __series('Candlestick').markers as Array<{ time: string }>;
    expect(markers.map((marker) => marker.time)).toContain('2026-05-28');
  });

  it('marks the session a quality finding concerns rather than smoothing it', () => {
    mountChart({ findings: [buildFinding({ sessionDate: '2026-05-29' })] });
    const markers = __series('Candlestick').markers as Array<{ time: string }>;
    expect(markers.map((marker) => marker.time)).toContain('2026-05-29');
  });

  it('disposes the chart when it goes away so a remount does not leak one', () => {
    const wrapper = mountChart();
    const chart = __lastChart();
    wrapper.unmount();
    expect(chart.removed).toBe(true);
  });
});

describe('PriceChart theming', () => {
  beforeEach(() => {
    __reset();
    document.documentElement.className = '';
    document.documentElement.removeAttribute('style');
  });

  it('gives the canvas real colours, because a canvas cannot resolve currentColor', () => {
    mountChart();
    const layout = __lastChart().options.layout as { textColor?: string };
    // `currentColor` is a CSS keyword. Canvas rendering silently ignores it and falls back to
    // whatever the library last used, which is how the axis labels ended up unrelated to the
    // theme.
    expect(layout.textColor).toBeDefined();
    expect(layout.textColor).not.toBe('currentColor');
    expect(layout.textColor).toMatch(/^(#|rgb|hsl)/);
  });

  it('draws grid lines from the theme rather than the library default', () => {
    document.documentElement.style.setProperty('--chart-grid', 'rgb(1, 2, 3)');
    mountChart();
    const grid = __lastChart().options.grid as {
      vertLines?: { color?: string }; horzLines?: { color?: string };
    };
    // The default grid is tuned for a light background and reads as near-white on dark, where
    // it drowns the candles it is supposed to sit behind.
    expect(grid?.horzLines?.color).toBe('rgb(1, 2, 3)');
    expect(grid?.vertLines?.color).toBe('rgb(1, 2, 3)');
  });

  it('falls back to a readable grid for the active theme when no token is set', () => {
    document.documentElement.classList.add('market-lens-dark');
    mountChart();
    const grid = __lastChart().options.grid as { horzLines?: { color?: string } };
    expect(grid?.horzLines?.color).toBeDefined();
    // Whatever the fallback is, it must not be the opaque near-white that caused the problem.
    expect(grid!.horzLines!.color).not.toMatch(/^#(f{3,6}|FFF)/);
  });

  it('re-reads its colours when the theme changes, without remounting', async () => {
    document.documentElement.style.setProperty('--chart-grid', 'rgb(1, 2, 3)');
    mountChart();
    const chart = __lastChart();
    const before = chart.applied.length;

    document.documentElement.style.setProperty('--chart-grid', 'rgb(9, 9, 9)');
    document.documentElement.classList.add('market-lens-dark');
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(chart.applied.length, 'the chart kept its old colours after a theme change')
      .toBeGreaterThan(before);
    const latest = chart.applied.at(-1) as { grid?: { horzLines?: { color?: string } } };
    expect(latest?.grid?.horzLines?.color).toBe('rgb(9, 9, 9)');
  });

  it('puts volume in its own band instead of behind the candles', () => {
    mountChart();
    const scales = __lastChart().priceScales;
    const volume = scales.volume as { scaleMargins?: { top?: number; bottom?: number } };
    expect(volume, 'volume has no price scale of its own').toBeDefined();
    // Without this the bars run the full height of the chart, overlapping the candles and
    // colliding with the price axis labels.
    expect(volume.scaleMargins?.top).toBeGreaterThanOrEqual(0.7);
  });

  it('keeps the volume badge off the price axis it does not belong to', () => {
    mountChart();
    const volume = __series('Histogram');
    // Volume is measured on its own scale, so its last-value badge lands on top of a price
    // label and its price line draws a stray rule across the candles. Both were visible in
    // light and dark alike.
    expect(volume.options.lastValueVisible).toBe(false);
    expect(volume.options.priceLineVisible).toBe(false);
  });

  it('keeps the price badge, which is the one worth reading', () => {
    mountChart();
    expect(__series('Candlestick').options.lastValueVisible).not.toBe(false);
  });

  it('leaves room under the candles for that band', () => {
    mountChart();
    const right = __lastChart().priceScales.right as { scaleMargins?: { bottom?: number } };
    expect(right?.scaleMargins?.bottom).toBeGreaterThan(0);
  });
});
