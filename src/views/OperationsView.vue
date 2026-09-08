<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import Message from 'primevue/message';
import MarketDataStatus from '@/components/finance/MarketDataStatus.vue';
import FeatureRunList from '@/components/finance/FeatureRunList.vue';
import StrategyRunList from '@/components/finance/StrategyRunList.vue';
import QualityFindingList from '@/components/finance/QualityFindingList.vue';
import {
  MarketDataLive,
  fetchFeatureRuns,
  fetchRecentImports,
  fetchStrategyRuns,
  fetchFindingsAwaitingDecision,
  acceptFinding,
  type LiveEventSource,
} from '@/services/marketData';
import { createCoalescer } from '@/services/coalesce';
import { authStore } from '@/stores/auth';
import type {
  ConnectionState,
  FeatureRunSummary,
  ImportRunSummary,
  StrategyRunSummary,
  QualityFinding,
} from '@/types/marketData';

/**
 * Operational state: did the data arrive, and was it turned into the numbers the market
 * screens show.
 *
 * This lives apart from Market Data on purpose. Stacked under the instrument table it served
 * neither reader: somebody researching scrolled past it, and somebody checking last night's
 * import had to scroll the entire universe to find it.
 */
const imports = ref<ImportRunSummary[]>([]);
const runs = ref<FeatureRunSummary[]>([]);
const strategyRuns = ref<StrategyRunSummary[]>([]);
const findings = ref<QualityFinding[]>([]);
const findingsBusy = ref(false);
const loading = ref(true);
const importError = ref('');
const runError = ref('');
const strategyError = ref('');
const findingError = ref('');
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');

let controller: AbortController | undefined;

async function load(): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  // The two reads are independent: one failing must not hide the other, because either alone
  // still answers a question somebody came here to ask.
  const [importResult, runResult, strategyResult, findingResult] = await Promise.allSettled([
    fetchRecentImports(fetch, signal),
    fetchFeatureRuns(fetch, signal),
    fetchStrategyRuns(fetch, signal),
    fetchFindingsAwaitingDecision(fetch, signal),
  ]);
  if (signal.aborted) return;
  if (importResult.status === 'fulfilled') {
    imports.value = importResult.value;
    importError.value = '';
  } else {
    importError.value = 'Unable to load recent market-data imports.';
  }
  if (runResult.status === 'fulfilled') {
    runs.value = runResult.value;
    runError.value = '';
  } else {
    runError.value = 'Unable to load recent feature runs.';
  }
  if (strategyResult.status === 'fulfilled') {
    strategyRuns.value = strategyResult.value;
    strategyError.value = '';
  } else {
    strategyError.value = 'Unable to load recent strategy runs.';
  }
  if (findingResult.status === 'fulfilled') {
    findings.value = findingResult.value;
    findingError.value = '';
  } else {
    findingError.value = 'Unable to load the findings awaiting a decision.';
  }
  loading.value = false;
}

/**
 * The screen this report moved from was live, and moving it must not make it stale. An import
 * that finishes while somebody is watching updates here, coalesced because a running import
 * emits one event per instrument.
 */
/**
 * Accepting is the owner's judgement, so it needs the double-submit token a mutation carries. The
 * list is re-read rather than patched: the server decides what is still awaiting a decision.
 */
async function accept(findingID: string): Promise<void> {
  findingsBusy.value = true;
  try {
    await acceptFinding(findingID, authStore.requireCSRF('accept a finding'));
    await load();
  } catch {
    findingError.value = 'Unable to accept this finding.';
  } finally {
    findingsBusy.value = false;
  }
}

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
  <div class="operations">
    <header class="page-intro">
      <p class="eyebrow">Operations</p>
      <h1>Data pipeline</h1>
      <p class="operations__lead">
        Whether the market data arrived, and whether it was turned into the statistics the
        market screens show.
      </p>
    </header>

    <Message
      v-if="!loading && imports.length === 0 && !importError"
      severity="info"
      :closable="false"
    >
      No import has run in this deployment yet. Until one does, there is no market data to
      compute from.
    </Message>

    <MarketDataStatus
      :runs="imports"
      :connection-state="connectionState"
      :loading="loading"
      :error="importError"
    />

    <FeatureRunList :runs="runs" :loading="loading" :error="runError" />

    <StrategyRunList :runs="strategyRuns" :loading="loading" :error="strategyError" />

    <QualityFindingList
      :findings="findings"
      :loading="loading"
      :error="findingError"
      :busy="findingsBusy"
      @accept="accept"
    />
  </div>
</template>

<style scoped>
.operations {
  display: flex;
  flex-direction: column;
  gap: 2rem;
}

.operations__lead {
  color: var(--p-text-muted-color);
  margin: 0;
  max-width: 60ch;
}

</style>
