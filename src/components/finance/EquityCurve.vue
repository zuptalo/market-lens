<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { AreaSeries, createChart } from 'lightweight-charts';
import Message from 'primevue/message';
import type { BacktestEquityPoint } from '@/types/marketData';

/**
 * The simulated portfolio's value over time.
 *
 * The chart is the second way this component says what it has to say. The first is the text
 * beneath it, which carries every figure the shape conveys — start, end, high, low, and how many
 * sessions could not be valued. A reader who cannot see a canvas is not reading a poorer version
 * of the result, and that is a requirement rather than a courtesy.
 *
 * Two things it must never do, for the same reason the price chart must not:
 *
 *  - Bridge a session that could not be valued. Those become whitespace, so the line breaks
 *    rather than joining the sessions either side into a slope nobody observed.
 *  - Redraw a stale point as though it were current. A session with no total has no point.
 */

const props = withDefaults(defineProps<{
  points: BacktestEquityPoint[];
  currency: string;
  height?: number;
}>(), { height: 280 });

const container = ref<HTMLDivElement>();
// The chart is drawn only where there is room for it to mean something. Below this the shape is
// a squiggle and the figures below carry the whole result, which they do at every width anyway.
const CHART_FLOOR = 640;
const wideEnough = ref(true);

const valued = computed(() => props.points.filter((point) => point.total !== null));

const summary = computed(() => {
  const totals = valued.value.map((point) => ({ session: point.sessionDate, value: Number(point.total) }))
    .filter((point) => Number.isFinite(point.value));
  if (totals.length === 0) return null;
  let high = totals[0];
  let low = totals[0];
  for (const point of totals) {
    if (point.value > high.value) high = point;
    if (point.value < low.value) low = point;
  }
  return { first: totals[0], last: totals[totals.length - 1], high, low, count: totals.length };
});

const unvalued = computed(() => props.points.length - valued.value.length);

function money(value: number): string {
  return `${value.toLocaleString(undefined, { maximumFractionDigits: 0 })} ${props.currency}`;
}

let chart: ReturnType<typeof createChart> | undefined;
let series: ReturnType<ReturnType<typeof createChart>['addSeries']> | undefined;

function colour(name: string, fallback: string): string {
  if (typeof getComputedStyle !== 'function' || !container.value) return fallback;
  const value = getComputedStyle(container.value).getPropertyValue(name).trim();
  return value === '' ? fallback : value;
}

function draw(): void {
  if (!container.value || !wideEnough.value) return;
  if (!chart) {
    chart = createChart(container.value, {
      height: props.height,
      autoSize: true,
      layout: {
        background: { color: 'transparent' },
        textColor: colour('--p-text-muted-color', '#6b7280'),
        attributionLogo: true,
      },
      grid: {
        vertLines: { color: colour('--p-content-border-color', '#e5e7eb') },
        horzLines: { color: colour('--p-content-border-color', '#e5e7eb') },
      },
      rightPriceScale: { borderVisible: false },
      timeScale: { borderVisible: false },
    });
    series = chart.addSeries(AreaSeries, {
      lineColor: colour('--p-primary-color', '#4f46e5'),
      topColor: colour('--p-primary-color', '#4f46e5'),
      bottomColor: 'transparent',
      priceLineVisible: false,
    });
  }
  // A session that could not be valued becomes whitespace: the line breaks there rather than
  // drawing a slope across a gap nobody observed.
  series?.setData(props.points.map((point) => (point.total === null
    ? { time: point.sessionDate }
    : { time: point.sessionDate, value: Number(point.total) })));
  chart?.timeScale().fitContent();
}

function measure(): void {
  wideEnough.value = typeof window === 'undefined' ? true : window.innerWidth >= CHART_FLOOR;
  if (!wideEnough.value && chart) {
    chart.remove();
    chart = undefined;
    series = undefined;
  }
  if (wideEnough.value) draw();
}

onMounted(() => {
  measure();
  window.addEventListener('resize', measure);
});

onBeforeUnmount(() => {
  window.removeEventListener('resize', measure);
  chart?.remove();
  chart = undefined;
  series = undefined;
});

watch(() => props.points, draw, { deep: false });
</script>

<template>
  <section class="equity" aria-labelledby="equity-heading">
    <h3 id="equity-heading">What the portfolio was worth</h3>

    <div
      v-if="wideEnough && summary"
      ref="container"
      class="equity__chart"
      data-testid="equity-chart"
      role="img"
      :aria-label="`Simulated portfolio value from ${summary.first.session} to ${summary.last.session}, ending at ${money(summary.last.value)}. The figures are listed below.`"
    />

    <Message v-if="!summary" severity="info" :closable="false">
      No session in this range could be valued, so there is no curve to show.
    </Message>

    <!-- Every figure the shape conveys, as text. This is the result; the chart is a second way
         of looking at it. -->
    <dl v-else class="equity__figures" data-testid="equity-figures">
      <div>
        <dt>Started</dt>
        <dd>{{ money(summary.first.value) }} <span>on {{ summary.first.session }}</span></dd>
      </div>
      <div>
        <dt>Ended</dt>
        <dd>{{ money(summary.last.value) }} <span>on {{ summary.last.session }}</span></dd>
      </div>
      <div>
        <dt>Highest</dt>
        <dd>{{ money(summary.high.value) }} <span>on {{ summary.high.session }}</span></dd>
      </div>
      <div>
        <dt>Lowest</dt>
        <dd>{{ money(summary.low.value) }} <span>on {{ summary.low.session }}</span></dd>
      </div>
      <div>
        <dt>Sessions valued</dt>
        <dd>{{ summary.count }}</dd>
      </div>
      <div v-if="unvalued > 0">
        <dt>Sessions that could not be valued</dt>
        <dd>
          {{ unvalued }}
          <span>a holding had no price or no rate, so the total is not stated for them</span>
        </dd>
      </div>
    </dl>
  </section>
</template>

<style scoped>
.equity__chart {
  width: 100%;
  min-height: 280px;
  margin-bottom: 1rem;
}

.equity__figures {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 14rem), 1fr));
  gap: 0.75rem 1.5rem;
  margin: 0;
}

.equity__figures dt {
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.equity__figures dd {
  margin: 0;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.equity__figures dd span {
  display: block;
  font-weight: 400;
  font-size: 0.8125rem;
  color: var(--p-text-muted-color);
}
</style>
