<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import Message from 'primevue/message';
import Tag from 'primevue/tag';
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import { RouterLink } from 'vue-router';
import {
  MarketDataLive,
  fetchSignalRanking,
  type LiveEventSource,
} from '@/services/marketData';
import { createCoalescer } from '@/services/coalesce';
import type { ConnectionState, RankedSignal, SignalAction, StrategyRef } from '@/types/marketData';

/**
 * The universe in a strategy's order.
 *
 * Instruments the strategy could not score are in the same list, below the scored ones and
 * clearly marked, rather than filtered away. Hiding them would make the ranking look like the
 * whole universe when it is not, and putting them at the bottom without a marker would make an
 * absence read as the weakest view in the list.
 */
const items = ref<RankedSignal[]>([]);
const strategy = ref<StrategyRef | null>(null);
const sessionDate = ref('');
const scored = ref(0);
const unscored = ref(0);
const total = ref<number | null>(null);
const loading = ref(true);
const error = ref('');
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');

let controller: AbortController | undefined;

const scoredItems = computed(() => items.value.filter((item) => item.score !== null));
const unscoredItems = computed(() => items.value.filter((item) => item.score === null));

async function load(): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  try {
    const page = await fetchSignalRanking({ limit: 200 }, fetch, signal);
    if (signal.aborted) return;
    items.value = page.items;
    strategy.value = page.strategy;
    sessionDate.value = page.sessionDate;
    scored.value = page.scored;
    unscored.value = page.unscored;
    total.value = page.total;
    error.value = '';
  } catch {
    if (signal.aborted) return;
    error.value = 'Unable to load the strategy ranking.';
  } finally {
    if (!signal.aborted) loading.value = false;
  }
}

function severity(action: SignalAction): 'success' | 'info' | 'warn' | 'danger' | 'secondary' {
  switch (action) {
    case 'BUY': return 'success';
    case 'WATCH': return 'info';
    case 'HOLD': return 'secondary';
    case 'REDUCE': return 'warn';
    default: return 'danger';
  }
}

const absenceWording: Record<string, string> = {
  insufficient_history: 'too little stored history',
  feature_unavailable: 'no usable feature value',
  composite_undefined: 'the universe composite was undefined',
  liquidity_excluded: 'excluded by the liquidity rule',
};

function absence(reason: string | null): string {
  if (reason === null) return 'no reason recorded';
  return absenceWording[reason] ?? reason.replace(/_/g, ' ');
}

function decimal(value: string | null): string {
  return value === null ? '—' : Number(value).toFixed(2);
}

function agreement(value: string | null): string {
  return value === null ? '—' : `${(Number(value) * 100).toFixed(0)}%`;
}

/**
 * A recomputation arrives as signals.changed.v1. The whole page is re-read rather than patched:
 * the factors are cross-sectional, so one instrument's change moves every other instrument's
 * rank, and patching one row would leave the rest of the ranking quietly wrong.
 */
const refresh = createCoalescer(async () => { await load(); });

function browserEventSource(url: string, lastEventId: string): LiveEventSource {
  const endpoint = lastEventId ? `${url}?last_event_id=${encodeURIComponent(lastEventId)}` : url;
  return new EventSource(endpoint, { withCredentials: true }) as unknown as LiveEventSource;
}

const live = new MarketDataLive({
  sourceFactory: browserEventSource,
  onRefresh: (entityType) => { refresh.add(entityType); },
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
  refresh.cancel();
  live.stop();
  window.removeEventListener('online', online);
  window.removeEventListener('offline', offline);
});
</script>

<template>
  <div class="signals">
    <header class="page-intro">
      <p class="eyebrow">Strategies</p>
      <h1>The universe in a strategy's order</h1>
      <p v-if="strategy" class="signals__lead">
        {{ strategy.title }} ({{ strategy.name }} v{{ strategy.version }}) as of
        {{ sessionDate }}. {{ scored }} instrument<span v-if="scored !== 1">s</span> scored,
        {{ unscored }} not.
      </p>
    </header>

    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <Message
      v-else-if="!loading && items.length === 0"
      severity="info"
      :closable="false"
    >
      No strategy has recorded a view of this universe yet. Signals are computed after the
      features they read, so a deployment that has not run one has none.
    </Message>

    <template v-else>
      <DataTable
        :value="scoredItems"
        :loading="loading"
        data-testid="signal-ranking"
        responsive-layout="stack"
        breakpoint="768px"
        class="signals__table"
      >
        <Column header="#" :pt="{ bodyCell: { 'data-label': 'Rank' } }">
          <template #body="{ data }">{{ data.rank }}</template>
        </Column>
        <Column field="ticker" header="Instrument" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
          <template #body="{ data }">
            <RouterLink :to="`/markets/${data.instrumentId}`" class="signals__link">
              {{ data.ticker }}
            </RouterLink>
            <span class="signals__name">{{ data.name }}</span>
          </template>
        </Column>
        <Column field="action" header="View" :pt="{ bodyCell: { 'data-label': 'View' } }">
          <template #body="{ data }">
            <Tag :severity="severity(data.action)" :value="data.action" />
          </template>
        </Column>
        <Column header="Score" :pt="{ bodyCell: { 'data-label': 'Score' } }">
          <template #body="{ data }">{{ decimal(data.score) }}</template>
        </Column>
        <Column header="Factor agreement" :pt="{ bodyCell: { 'data-label': 'Factor agreement' } }">
          <template #body="{ data }">{{ agreement(data.confidence) }}</template>
        </Column>
      </DataTable>

      <section v-if="unscoredItems.length > 0" class="signals__unscored" aria-labelledby="unscored-heading">
        <h2 id="unscored-heading">Not scored</h2>
        <p class="signals__unscored-lead">
          These instruments are part of the universe. The strategy formed no view of them, which
          is not the same as a weak one.
        </p>
        <DataTable
          :value="unscoredItems"
          data-testid="unscored-instruments"
          responsive-layout="stack"
          breakpoint="768px"
        >
          <Column field="ticker" header="Instrument" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
            <template #body="{ data }">
              <RouterLink :to="`/markets/${data.instrumentId}`" class="signals__link">
                {{ data.ticker }}
              </RouterLink>
              <span class="signals__name">{{ data.name }}</span>
            </template>
          </Column>
          <Column header="Why" :pt="{ bodyCell: { 'data-label': 'Why' } }">
            <template #body="{ data }">{{ absence(data.absenceReason) }}</template>
          </Column>
        </DataTable>
      </section>

      <p v-if="strategy" class="signals__caveat">
        This is a strategy output, not advice. {{ strategy.caveat }}
      </p>
    </template>
  </div>
</template>

<style scoped>
.signals {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}

.signals__lead,
.signals__unscored-lead,
.signals__caveat {
  color: var(--p-text-muted-color);
  margin: 0;
  max-width: 70ch;
}

.signals__caveat {
  font-size: 0.875rem;
}

.signals__name {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
}

.signals__link {
  font-weight: 600;
}

.signals__table,
.signals__unscored {
  --p-datatable-row-padding: 0.75rem;
}

.signals__unscored {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.signals__unscored h2 {
  margin: 0;
  font-size: 1.05rem;
}
</style>
