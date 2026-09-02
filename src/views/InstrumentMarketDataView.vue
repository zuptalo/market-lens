<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import ChartRangeControls from '@/components/finance/ChartRangeControls.vue';
import ChartAnnotations from '@/components/finance/ChartAnnotations.vue';
import PriceChart from '@/components/finance/PriceChart.vue';
import MarketDataStatus from '@/components/finance/MarketDataStatus.vue';
import SignalCard from '@/components/finance/SignalCard.vue';
import ContributionList from '@/components/finance/ContributionList.vue';
import {
  MarketDataLive,
  fetchInstrumentHistory,
  fetchInstrumentSignal,
  type LiveEvent,
  type LiveEventPayload,
  type LiveEventSource,
} from '@/services/marketData';
import { createCoalescer } from '@/services/coalesce';
import type { ConnectionState, HistoryWindow, Signal } from '@/types/marketData';

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
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');
const signal_ = ref<Signal | null>(null);

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
    // The signal is read separately and its failure is swallowed: the history is what this
    // screen is for, and an unavailable strategy layer must not take the chart down with it.
    try {
      signal_.value = await fetchInstrumentSignal(instrumentId, {}, fetch, request.signal);
    } catch {
      signal_.value = null;
    }
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

function browserEventSource(url: string, lastEventId: string): LiveEventSource {
  const endpoint = lastEventId ? `${url}?last_event_id=${encodeURIComponent(lastEventId)}` : url;
  const source = new EventSource(endpoint);
  return {
    addEventListener(type, listener) {
      source.addEventListener(type, (event) => {
        const message = event as MessageEvent<string>;
        listener({ lastEventId: message.lastEventId ?? '', type, data: message.data ?? '' } satisfies LiveEvent);
      });
    },
    close: () => source.close(),
  };
}

/**
 * Reload the window only when the change concerns *this* instrument, and only once for a
 * burst of them.
 *
 * An import over a ten-year history publishes one event per bar. Reloading per event opened
 * thousands of overlapping requests and froze the page, so the events are collected and
 * answered with a single reload.
 *
 * The reload replaces the bars; the person's selected range, overlay choices and connection
 * state live outside `window_` and are therefore untouched, and the chart keeps its own
 * visible window because the component is updated rather than remounted (FR-020).
 */
const windowRefresh = createCoalescer(async () => { await load(); });

function applyLiveChange(payload: LiveEventPayload): void {
  if (payload.instrument_id && payload.instrument_id !== instrumentId) return;
  windowRefresh.add(instrumentId);
}

const live = new MarketDataLive({
  sourceFactory: browserEventSource,
  onRefresh: (_entityType, _entityId, payload) => applyLiveChange(payload),
  onState: (state) => { connectionState.value = state; },
  reconnectDelayMs: 1_000,
  staleAfterMs: 10_000,
});
const online = () => live.setOnline(true);
const offline = () => live.setOnline(false);

onMounted(() => {
  void load();
  live.setOnline(navigator.onLine);
  live.start();
  window.addEventListener('online', online);
  window.addEventListener('offline', offline);
});

onBeforeUnmount(() => {
  controller?.abort();
  windowRefresh.cancel();
  live.stop();
  window.removeEventListener('online', online);
  window.removeEventListener('offline', offline);
});
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

      <section class="signal-panel" aria-labelledby="signal-heading">
        <h2 id="signal-heading">Strategy view</h2>
        <div class="signal-panel__body">
          <SignalCard :signal="signal_" />
          <ContributionList
            v-if="signal_ && signal_.contributions.length > 0"
            :contributions="signal_.contributions"
          />
        </div>
      </section>

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
        <!--
          The facts the chart cannot show, ordered as a reader needs them: what is on screen,
          what exists behind it, whether anything is missing from it, and only then where it
          came from. Provenance matters but is not what someone opens the page to learn.
        -->
        <dl class="window-facts">
          <div class="window-facts__item">
            <dt>Showing</dt>
            <dd>
              <span class="window-facts__value">{{ window_.requestedFrom ?? '—' }} to {{ window_.requestedTo ?? '—' }}</span>
              <span class="window-facts__note">{{ window_.bars.length }} stored sessions</span>
            </dd>
          </div>
          <div class="window-facts__item">
            <dt>Stored coverage</dt>
            <dd>
              <span class="window-facts__value">{{ window_.coverage.firstSession ?? '—' }} to {{ window_.coverage.lastSession ?? '—' }}</span>
              <span class="window-facts__note">{{ window_.coverage.storedSessions }} sessions in total</span>
            </dd>
          </div>
          <div class="window-facts__item">
            <dt>Missing in view</dt>
            <dd>
              <span class="window-facts__value">
                {{ missingInView === 0 ? 'None' : `${missingInView} sessions` }}
              </span>
              <span class="window-facts__note">
                <template v-if="missingInView === 0">every open session in view is stored</template>
                <template v-else>the exchange was open with no stored bar</template>
              </span>
            </dd>
          </div>
          <div class="window-facts__item">
            <dt>Series basis</dt>
            <dd><span class="window-facts__value">{{ seriesBasisLabel }}</span></dd>
          </div>
          <div v-if="window_.provider" class="window-facts__item">
            <dt>Provider</dt>
            <dd>
              <span class="window-facts__value">{{ window_.provider }}</span>
              <span v-if="observedAtLabel" class="window-facts__note">observed {{ observedAtLabel }}</span>
            </dd>
          </div>
        </dl>

        <p v-if="window_.coverage.storedSessions === 0" class="empty-state" role="status">
          No daily history is stored for this instrument yet. Nothing is drawn rather than
          estimated.
        </p>
      </section>

      <MarketDataStatus
        :runs="[]"
        :connection-state="connectionState"
        :loading="false"
        error=""
      />

      <ChartAnnotations
        :actions="window_.actions"
        :findings="openFindings"
        :missing-sessions="window_.missingSessions"
      />
    </template>
  </div>
</template>

<style scoped>
.identity-facts {
  display: grid;
  gap: 0.5rem 1.5rem;
  grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr));
  margin: 0;
}

.identity-facts div {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.identity-facts dt {
  font-weight: 600;
  opacity: 0.75;
  margin: 0;
}

.identity-facts dd {
  margin: 0;
}

/*
 * Each fact is a labelled block rather than a row of loose text. The label is small and
 * quiet, the value is the thing being read, and its qualifier sits underneath — so the eye
 * lands on the answer rather than on the word "Showing". Separated from the chart by a rule,
 * because these are about the data and not part of the picture.
 */
.window-facts {
  display: grid;
  gap: 1rem 2rem;
  grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
  margin: 1rem 0 0;
  padding-block-start: 1rem;
  border-block-start: 1px solid var(--p-content-border-color, rgb(0 0 0 / 0.12));
}

.window-facts__item dt {
  margin: 0 0 0.15rem;
  font-size: 0.8rem;
  font-weight: 600;
  letter-spacing: 0.02em;
  text-transform: uppercase;
  opacity: 0.6;
}

.window-facts__item dd {
  margin: 0;
  display: grid;
  gap: 0.1rem;
}

.window-facts__value {
  font-variant-numeric: tabular-nums;
}

.window-facts__note {
  font-size: 0.85em;
  opacity: 0.7;
}

.chart-panel {
  margin-block: 1.5rem;
}

.signal-panel {
  margin-block: 1.5rem;
}

.signal-panel__body {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.signal-panel h2 {
  margin: 0;
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
      'signal signal'
      'chart context';
    gap: 0 2rem;
    align-items: start;
  }

  .back-link { grid-area: back; }
  .identity { grid-area: identity; }
  .chart-panel { grid-area: chart; }

  /*
    The strategy view spans both columns. Auto-placed it landed in the narrow annotation
    column, where "raises the score by 0.238" wrapped onto five lines — the contribution table
    is the one thing on this screen that must stay readable as a sentence, because it is the
    reason the score is not an oracle.
  */
  .signal-panel { grid-area: signal; }

  .signal-panel__body {
    display: grid;
    grid-template-columns: minmax(18rem, 1fr) minmax(0, 2fr);
    gap: 2rem;
    align-items: start;
  }
}
</style>
