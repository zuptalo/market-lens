<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import {
  CandlestickSeries,
  HistogramSeries,
  LineSeries,
  createChart,
  createSeriesMarkers,
} from 'lightweight-charts';
import { movingAverage } from './movingAverage';
import { formatDecimal } from '@/utils/decimal';
import type { Bar, CorporateAction, QualityFinding } from '@/types/marketData';

/**
 * The price chart — the only file in the client that knows the charting library exists.
 *
 * Everything the library needs is built here and nothing of it escapes: no caller passes in
 * one of its types, and no caller receives one. If the licence ever becomes unacceptable or
 * the library is replaced, this file is the whole cost.
 *
 * Two things it must never do:
 *
 *  - Bridge a gap. Sessions the exchange was open for with no stored bar are handed to the
 *    library as whitespace points, which is what makes it interrupt the series rather than
 *    joining the sessions either side into a line that implies an observation (FR-013).
 *  - Draw a missing volume as zero. Zero volume is an observation; absence is not.
 *
 * The TradingView attribution is set explicitly rather than left to the library's default,
 * because the licence requires the link and a later edit tidying the chart up would
 * otherwise breach it silently. A test asserts it stays on.
 */

const props = withDefaults(defineProps<{
  bars: Bar[];
  missingSessions: string[];
  overlays: number[];
  actions?: CorporateAction[];
  findings?: QualityFinding[];
  height?: number;
}>(), { actions: () => [], findings: () => [], height: 360 });

const emit = defineEmits<{
  (event: 'window-change', value: { from: string; to: string } | null): void;
}>();

const container = ref<HTMLDivElement>();

type Chart = ReturnType<typeof createChart>;
type Series = ReturnType<Chart['addSeries']>;

let chart: Chart | undefined;
let candles: Series | undefined;
let volume: Series | undefined;
let overlaySeries: Series[] = [];

/**
 * Merge the stored bars with the sessions that are absent, in date order. An absent session
 * becomes a point carrying only its time, which the library renders as a break.
 */
function withGaps<T extends { time: string }>(points: T[], missing: string[]): Array<T | { time: string }> {
  const merged: Array<T | { time: string }> = [...points, ...missing.map((time) => ({ time }))];
  merged.sort((left, right) => (left.time < right.time ? -1 : left.time > right.time ? 1 : 0));
  return merged;
}

function candleData() {
  return withGaps(props.bars.map((bar) => ({
    time: bar.sessionDate,
    open: Number(bar.open),
    high: Number(bar.high),
    low: Number(bar.low),
    close: Number(bar.close),
  })), props.missingSessions);
}

function volumeData() {
  return withGaps(props.bars.map((bar) => ({
    time: bar.sessionDate,
    value: bar.volume,
  })), props.missingSessions);
}

/** Markers for the things that would otherwise look like a real move. */
function markers() {
  const stored = new Set(props.bars.map((bar) => bar.sessionDate));
  const entries = [
    ...props.actions
      .filter((action) => stored.has(action.exDate))
      .map((action) => ({
        time: action.exDate,
        position: 'aboveBar' as const,
        shape: 'arrowDown' as const,
        color: 'currentColor',
        text: actionLabel(action),
      })),
    ...props.findings
      .filter((finding) => finding.sessionDate !== null && stored.has(finding.sessionDate))
      .map((finding) => ({
        time: finding.sessionDate as string,
        position: 'belowBar' as const,
        shape: 'circle' as const,
        color: 'currentColor',
        text: finding.rule,
      })),
  ];
  entries.sort((left, right) => (left.time < right.time ? -1 : left.time > right.time ? 1 : 0));
  return entries;
}

function actionLabel(action: CorporateAction): string {
  if (action.ratio) return `${action.actionType} ${formatDecimal(action.ratio, 0)}`;
  if (action.amount) return `${action.actionType} ${formatDecimal(action.amount)} ${action.currency ?? ''}`.trim();
  return action.actionType;
}

function renderOverlays(): void {
  if (!chart) return;
  for (const series of overlaySeries) chart.removeSeries(series);
  overlaySeries = [];
  for (const windowLength of props.overlays) {
    const series = chart.addSeries(LineSeries, { lineWidth: 2, priceLineVisible: false });
    series.setData(
      movingAverage(props.bars, windowLength, props.missingSessions)
        // A point with no value breaks the line; a point omitted entirely would let the
        // library join across the hole.
        .map((point) => (point.value === null
          ? { time: point.sessionDate }
          : { time: point.sessionDate, value: point.value })),
    );
    overlaySeries.push(series);
  }
}

function render(): void {
  if (!chart || !candles || !volume) return;
  candles.setData(candleData());
  volume.setData(volumeData());
  createSeriesMarkers(candles, markers());
  renderOverlays();
}

onMounted(() => {
  if (!container.value) return;
  chart = createChart(container.value, {
    height: props.height,
    layout: {
      // Required by the library's licence. Do not turn this off.
      attributionLogo: true,
      // Theme tokens are passed at runtime rather than shipped as three stylesheets, so
      // system, light and dark all work without the chart knowing which is active.
      background: { color: 'transparent' },
      textColor: 'currentColor',
    },
    autoSize: true,
    timeScale: { borderVisible: false, fixLeftEdge: true, fixRightEdge: true },
    rightPriceScale: { borderVisible: false },
  });
  candles = chart.addSeries(CandlestickSeries, { priceLineVisible: false });
  volume = chart.addSeries(HistogramSeries, {
    priceScaleId: 'volume',
    priceFormat: { type: 'volume' },
  });
  render();

  chart.timeScale().subscribeVisibleTimeRangeChange((range) => {
    emit('window-change', range ? { from: String(range.from), to: String(range.to) } : null);
  });
});

watch(
  () => [props.bars, props.missingSessions, props.overlays, props.actions, props.findings],
  render,
  { deep: true },
);

onBeforeUnmount(() => {
  chart?.remove();
  chart = undefined;
  candles = undefined;
  volume = undefined;
  overlaySeries = [];
});

/** Move the visible window, clamped by the library to the data it holds (FR-011). */
function showRange(from: string, to: string): void {
  chart?.timeScale().setVisibleRange({ from, to });
}

function fit(): void {
  chart?.timeScale().fitContent();
}

defineExpose({ showRange, fit });
</script>

<template>
  <div
    ref="container"
    class="price-chart"
    role="img"
    :aria-label="`Candlestick and volume chart over ${props.bars.length} stored sessions`"
    :style="{ minHeight: `${props.height}px` }"
  />
</template>

<style scoped>
.price-chart {
  width: 100%;
  /* A legible minimum height even on the narrowest supported screen. */
  min-height: 260px;
}
</style>
