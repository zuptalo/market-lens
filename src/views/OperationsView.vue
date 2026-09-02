<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import Message from 'primevue/message';
import MarketDataStatus from '@/components/finance/MarketDataStatus.vue';
import FeatureRunList from '@/components/finance/FeatureRunList.vue';
import { fetchFeatureRuns, fetchRecentImports } from '@/services/marketData';
import type { FeatureRunSummary, ImportRunSummary } from '@/types/marketData';

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
const loading = ref(true);
const importError = ref('');
const runError = ref('');

let controller: AbortController | undefined;

async function load(): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  // The two reads are independent: one failing must not hide the other, because either alone
  // still answers a question somebody came here to ask.
  const [importResult, runResult] = await Promise.allSettled([
    fetchRecentImports(fetch, signal),
    fetchFeatureRuns(fetch, signal),
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
  loading.value = false;
}

onMounted(() => { void load(); });
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <div class="operations">
    <header class="operations__header">
      <p class="operations__eyebrow">Operations</p>
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
      connection-state="connected"
      :loading="loading"
      :error="importError"
    />

    <FeatureRunList :runs="runs" :loading="loading" :error="runError" />
  </div>
</template>

<style scoped>
.operations {
  display: flex;
  flex-direction: column;
  gap: 2rem;
  padding: 1rem;
}

.operations__eyebrow {
  color: var(--p-text-muted-color);
  letter-spacing: 0.08em;
  margin: 0;
  text-transform: uppercase;
  font-size: 0.75rem;
}

.operations__header h1 {
  margin: 0.25rem 0 0.5rem;
}

.operations__lead {
  color: var(--p-text-muted-color);
  margin: 0;
  max-width: 60ch;
}

@media (min-width: 768px) {
  .operations {
    padding: 1.5rem;
  }
}
</style>
