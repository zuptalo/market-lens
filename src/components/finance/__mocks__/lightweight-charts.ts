/**
 * A recording stub for `lightweight-charts`.
 *
 * The real library renders to a canvas, which jsdom cannot do and which would tell us
 * nothing anyway: the question these tests ask is not "did pixels appear" but "was the
 * chart handed exactly the stored observations, and nothing else". So this stub records
 * every call and lets a test assert on what `PriceChart.vue` passed down.
 *
 * Activate it from a test file with:
 *
 *   vi.mock('lightweight-charts', () => import('./__mocks__/lightweight-charts'));
 *
 * It is deliberately *not* registered globally in `vitest.config.ts`. A global alias would
 * make it impossible for any future test to exercise the real library, and would hide the
 * mocking from whoever reads the test.
 */

export interface RecordedSeries {
  kind: string;
  options: Record<string, unknown>;
  /** Every payload passed to setData, most recent last. */
  data: unknown[][];
  /** Every payload passed to update, in order. */
  updates: unknown[];
  markers: unknown[];
  applied: Record<string, unknown>[];
  removed: boolean;
}

export interface RecordedRange {
  from: unknown;
  to: unknown;
}

export interface RecordedChart {
  container: HTMLElement | string;
  options: Record<string, unknown>;
  /** Options applied to each price scale, keyed by its id. */
  priceScales: Record<string, Record<string, unknown>>;
  series: RecordedSeries[];
  applied: Record<string, unknown>[];
  /** Logical/visible ranges the component asked the time scale to show. */
  ranges: RecordedRange[];
  fitContentCalls: number;
  resized: Array<{ width: number; height: number }>;
  removed: boolean;
  /** Handlers the component subscribed for visible-range changes. */
  rangeHandlers: Array<(range: RecordedRange | null) => void>;
}

/** Every chart created since the last reset, in creation order. */
export const __charts: RecordedChart[] = [];

export function __reset(): void {
  __charts.length = 0;
}

/** The most recently created chart, which is what a single-chart test wants. */
export function __lastChart(): RecordedChart {
  const chart = __charts[__charts.length - 1];
  if (!chart) throw new Error('No chart was created. Did the component mount?');
  return chart;
}

/** The one series of a given kind, failing loudly when the assumption does not hold. */
export function __series(kind: string, chart: RecordedChart = __lastChart()): RecordedSeries {
  const matches = chart.series.filter((series) => series.kind === kind && !series.removed);
  if (matches.length !== 1) {
    throw new Error(`Expected exactly one live ${kind} series, found ${matches.length}.`);
  }
  return matches[0];
}

/** The data most recently handed to a series. */
export function __dataOf(kind: string, chart: RecordedChart = __lastChart()): unknown[] {
  const series = __series(kind, chart);
  return series.data[series.data.length - 1] ?? [];
}

// Series definitions are opaque sentinels in the real library too; all the component does is
// pass them back to addSeries, so a tagged object is a faithful stand-in.
export const CandlestickSeries = { type: 'Candlestick' } as const;
export const HistogramSeries = { type: 'Histogram' } as const;
export const LineSeries = { type: 'Line' } as const;
export const AreaSeries = { type: 'Area' } as const;
export const BarSeries = { type: 'Bar' } as const;
export const BaselineSeries = { type: 'Baseline' } as const;

export const ColorType = { Solid: 'solid', VerticalGradient: 'gradient' } as const;
export const CrosshairMode = { Normal: 0, Magnet: 1, Hidden: 2 } as const;
export const LineStyle = { Solid: 0, Dotted: 1, Dashed: 2, LargeDashed: 3, SparseDotted: 4 } as const;
export const PriceScaleMode = { Normal: 0, Logarithmic: 1, Percentage: 2, IndexedTo100: 3 } as const;

function makeSeries(kind: string, options: Record<string, unknown>): {
  record: RecordedSeries;
  api: Record<string, unknown>;
} {
  const record: RecordedSeries = {
    kind,
    options: { ...options },
    data: [],
    updates: [],
    markers: [],
    applied: [],
    removed: false,
  };
  const api = {
    setData: (data: unknown[]) => {
      // Copy so a later in-place mutation by the component cannot rewrite history and make
      // an assertion pass that should have failed.
      record.data.push([...data]);
    },
    update: (bar: unknown) => {
      record.updates.push(bar);
    },
    setMarkers: (markers: unknown[]) => {
      record.markers = [...markers];
    },
    applyOptions: (options: Record<string, unknown>) => {
      record.applied.push({ ...options });
      Object.assign(record.options, options);
    },
    options: () => ({ ...record.options }),
    priceScale: () => ({ applyOptions: () => {} }),
    createPriceLine: () => ({ applyOptions: () => {} }),
    removePriceLine: () => {},
    data: () => record.data[record.data.length - 1] ?? [],
    seriesType: () => kind,
    __record: record,
  };
  return { record, api };
}

export function createChart(
  container: HTMLElement | string,
  options: Record<string, unknown> = {},
): Record<string, unknown> {
  const record: RecordedChart = {
    container,
    options: structuredClone(options),
    series: [],
    priceScales: {},
    applied: [],
    ranges: [],
    fitContentCalls: 0,
    resized: [],
    removed: false,
    rangeHandlers: [],
  };
  __charts.push(record);

  const timeScale = {
    fitContent: () => {
      record.fitContentCalls += 1;
    },
    setVisibleRange: (range: RecordedRange) => {
      record.ranges.push({ ...range });
    },
    setVisibleLogicalRange: (range: RecordedRange) => {
      record.ranges.push({ ...range });
    },
    getVisibleRange: () => record.ranges[record.ranges.length - 1] ?? null,
    getVisibleLogicalRange: () => record.ranges[record.ranges.length - 1] ?? null,
    subscribeVisibleTimeRangeChange: (handler: (range: RecordedRange | null) => void) => {
      record.rangeHandlers.push(handler);
    },
    unsubscribeVisibleTimeRangeChange: (handler: (range: RecordedRange | null) => void) => {
      const index = record.rangeHandlers.indexOf(handler);
      if (index >= 0) record.rangeHandlers.splice(index, 1);
    },
    subscribeVisibleLogicalRangeChange: (handler: (range: RecordedRange | null) => void) => {
      record.rangeHandlers.push(handler);
    },
    unsubscribeVisibleLogicalRangeChange: (handler: (range: RecordedRange | null) => void) => {
      const index = record.rangeHandlers.indexOf(handler);
      if (index >= 0) record.rangeHandlers.splice(index, 1);
    },
    applyOptions: () => {},
    scrollToPosition: () => {},
    timeToCoordinate: () => 0,
  };

  return {
    addSeries: (definition: { type: string }, seriesOptions: Record<string, unknown> = {}) => {
      const { record: seriesRecord, api } = makeSeries(definition.type, seriesOptions);
      record.series.push(seriesRecord);
      return api;
    },
    removeSeries: (series: { __record?: RecordedSeries }) => {
      if (series.__record) series.__record.removed = true;
    },
    applyOptions: (next: Record<string, unknown>) => {
      record.applied.push(structuredClone(next));
      Object.assign(record.options, next);
    },
    options: () => structuredClone(record.options),
    timeScale: () => timeScale,
    priceScale: (id: string = 'right') => ({
      applyOptions: (options: Record<string, unknown>) => {
        record.priceScales[id] = { ...(record.priceScales[id] ?? {}), ...structuredClone(options) };
      },
      options: () => ({ ...(record.priceScales[id] ?? {}) }),
    }),
    resize: (width: number, height: number) => {
      record.resized.push({ width, height });
    },
    remove: () => {
      record.removed = true;
    },
    subscribeCrosshairMove: () => {},
    unsubscribeCrosshairMove: () => {},
    subscribeClick: () => {},
    unsubscribeClick: () => {},
    panes: () => [{ setHeight: () => {}, moveTo: () => {} }],
    __record: record,
  };
}

export function createSeriesMarkers(
  series: { __record?: RecordedSeries },
  markers: unknown[] = [],
): Record<string, unknown> {
  if (series.__record) series.__record.markers = [...markers];
  return {
    setMarkers: (next: unknown[]) => {
      if (series.__record) series.__record.markers = [...next];
    },
    markers: () => (series.__record ? series.__record.markers : []),
    detach: () => {},
  };
}

export function createTextWatermark(): Record<string, unknown> {
  return { applyOptions: () => {}, detach: () => {} };
}
