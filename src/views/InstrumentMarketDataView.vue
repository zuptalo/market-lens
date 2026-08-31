<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import ChartRangeControls from '@/components/finance/ChartRangeControls.vue';
import ChartAnnotations from '@/components/finance/ChartAnnotations.vue';
import PriceChart from '@/components/finance/PriceChart.vue';
import { fetchInstrumentHistory } from '@/services/marketData';
import type { HistoryWindow } from '@/types/marketData';

/**
 * One instrument's stored history.
 *
 * The chart is the centrepiece, but it is not the only way to read any of it. Everything the
 * chart conveys visually — the window shown, the coverage behind it, how many sessions are
 * missing from view, the corporate actions, the open findings — is also stated as text
 * (FR-017). A gap that exists only as a shape inside a canvas is invisible to anyone who is
 * not looking at the canvas.
 */

const route = useRoute();
const instrumentId = String(route.params.instrumentId);
const returnQuery = typeof route.query.return === 'string' ? route.query.return : '';
const backURL = `/markets${returnQuery ? `?${returnQuery}` : ''}`;

const window_ = ref<HistoryWindow | null>(null);
const loading = ref(true);
const error = ref('');
const sessions = ref(250);
const overlays = ref<number[]>([20]);
const chart = ref<InstanceType<typeof PriceChart>>();

let controller: AbortController | undefined;

async function load(): Promise<void> {
  controller?.abort();
  const request = new AbortController();
  controller = request;
  loading.value = true;
  error.value = '';
  try {
    window_.value = await fetchInstrumentHistory(
      instrumentId, { sessions: sessions.value }, fetch, request.signal,
    );
  } catch (cause) {
    if (request.signal.aborted) return;
    if (cause instanceof DOMException && cause.name === 'AbortError') return;
    // Never repeat what the server said; it can name internal state.
    error.value = 'Unable to load instrument market data.';
  } finally {
    if (!request.signal.aborted) loading.value = false;
  }
}

const identity = computed(() => window_.value?.instrument ?? null);

/** Missing sessions that fall inside the window actually drawn. */
const missingInView = computed(() => window_.value?.missingSessions.length ?? 0);

const openFindings = computed(() =>
  (window_.value?.findings ?? []).filter((finding) => finding.status === 'open'));

const seriesBasisLabel = computed(() =>
  window_.value?.seriesBasis === 'provider_adjusted'
    ? 'Provider-adjusted closes'
    : 'Raw closes as observed');

const observedAtLabel = computed(() => {
  const observed = window_.value?.observedAt;
  if (!observed) return null;
  return observed.replace('T', ' ').replace('Z', ' UTC');
});

function setRange(next: number): void {
  sessions.value = next;
  void load();
}

function toggleOverlay(windowLength: number): void {
  overlays.value = overlays.value.includes(windowLength)
    ? overlays.value.filter((entry) => entry !== windowLength)
    : [...overlays.value, windowLength];
}

/**
 * Zoom and pan move the visible window inside the stored coverage. The chart clamps to the
 * data it holds, so neither can walk off the end into a region where nothing was ever
 * observed (FR-011).
 */
function zoom(direction: 'in' | 'out'): void {
  const bars = window_.value?.bars ?? [];
  if (bars.length < 2) return;
  const factor = direction === 'in' ? 0.6 : 1.75;
  const span = Math.max(2, Math.round(bars.length * factor));
  const from = bars[Math.max(0, bars.length - span)].sessionDate;
  const to = bars[bars.length - 1].sessionDate;
  chart.value?.showRange(from, to);
}

function pan(direction: 'back' | 'forward'): void {
  const bars = window_.value?.bars ?? [];
  if (bars.length < 2) return;
  const step = Math.max(1, Math.round(bars.length * 0.25));
  const shift = direction === 'back' ? -step : step;
  const start = Math.min(Math.max(0, shift), bars.length - 2);
  chart.value?.showRange(bars[start].sessionDate, bars[bars.length - 1].sessionDate);
}

onMounted(() => { void load(); });
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <div class="instrument-detail-view">
    <a :href="backURL" class="back-link">← Back to instruments</a>

    <p v-if="loading" role="status">Loading instrument market data…</p>
    <p v-else-if="error" role="alert" class="status-error">{{ error }}</p>

    <template v-else-if="window_ && identity">
      <header class="identity">
        <h1>{{ identity.name }}</h1>
        <p class="identity-line">
          <strong>{{ identity.ticker }}</strong> · {{ identity.exchange.mic }}
          <span>({{ identity.exchange.name }})</span>
        </p>
        <dl class="identity-facts">
          <div><dt>ISIN</dt><dd>{{ identity.isin }}</dd></div>
          <div><dt>Currency</dt><dd>{{ identity.currency }}</dd></div>
          <div><dt>Country</dt><dd>{{ identity.country }}</dd></div>
          <div v-if="identity.sector"><dt>Sector</dt><dd>{{ identity.sector }}</dd></div>
          <div v-if="identity.industry"><dt>Industry</dt><dd>{{ identity.industry }}</dd></div>
          <div><dt>Status</dt><dd>{{ identity.status }}</dd></div>
        </dl>
      </header>

      <section class="chart-panel" aria-labelledby="chart-heading">
        <h2 id="chart-heading">Stored daily history</h2>

        <ChartRangeControls
          :sessions="sessions"
          :overlays="overlays"
          :coverage-sessions="window_.coverage.storedSessions"
          @range="setRange"
          @zoom="zoom"
          @pan="pan"
          @toggle-overlay="toggleOverlay"
        />

        <PriceChart
          ref="chart"
          :bars="window_.bars"
          :missing-sessions="window_.missingSessions"
          :overlays="overlays"
          :actions="window_.actions"
          :findings="window_.findings"
        />

        <!--
          The same facts as the chart, in words. This is not a fallback: it is the only form
          in which a screen reader, or anyone reading rather than looking, can get them.
        -->
        <dl class="window-facts">
          <div>
            <dt>Showing</dt>
            <dd>
              {{ window_.requestedFrom ?? '—' }} to {{ window_.requestedTo ?? '—' }}
              ({{ window_.bars.length }} stored sessions)
            </dd>
          </div>
          <div>
            <dt>Stored coverage</dt>
            <dd>
              {{ window_.coverage.firstSession ?? '—' }} to {{ window_.coverage.lastSession ?? '—' }}
              ({{ window_.coverage.storedSessions }} sessions)
            </dd>
          </div>
          <div>
            <dt>Missing in view</dt>
            <dd>
              <template v-if="missingInView === 0">None — every open session in view is stored</template>
              <template v-else>
                {{ missingInView }} sessions the exchange was open with no stored bar
              </template>
            </dd>
          </div>
          <div>
            <dt>Series basis</dt>
            <dd>{{ seriesBasisLabel }}</dd>
          </div>
          <div v-if="window_.provider">
            <dt>Provider</dt>
            <dd>{{ window_.provider }}<template v-if="observedAtLabel">, observed {{ observedAtLabel }}</template></dd>
          </div>
        </dl>

        <p v-if="window_.coverage.storedSessions === 0" class="empty-state" role="status">
          No daily history is stored for this instrument yet. Nothing is drawn rather than
          estimated.
        </p>
      </section>

      <ChartAnnotations
        :actions="window_.actions"
        :findings="openFindings"
        :missing-sessions="window_.missingSessions"
      />
    </template>
  </div>
</template>

<style scoped>
.identity-facts,
.window-facts {
  display: grid;
  gap: 0.5rem 1.5rem;
  grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr));
  margin: 0;
}

.identity-facts div,
.window-facts div {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.identity-facts dt,
.window-facts dt {
  font-weight: 600;
  opacity: 0.75;
  margin: 0;
}

.identity-facts dd,
.window-facts dd {
  margin: 0;
}

.chart-panel {
  margin-block: 1.5rem;
}

/*
 * Mobile gives the chart the full width of the panel. From tablet width up the identity
 * facts sit beside it, and at desktop width the annotation panels join them.
 */
@media (min-width: 768px) {
  .identity-facts {
    grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
  }
}

@media (min-width: 1024px) {
  .instrument-detail-view {
    display: grid;
    grid-template-columns: minmax(0, 3fr) minmax(16rem, 1fr);
    grid-template-areas:
      'back back'
      'identity identity'
      'chart context';
    gap: 0 2rem;
    align-items: start;
  }

  .back-link { grid-area: back; }
  .identity { grid-area: identity; }
  .chart-panel { grid-area: chart; }
}
</style>
