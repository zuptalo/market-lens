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
