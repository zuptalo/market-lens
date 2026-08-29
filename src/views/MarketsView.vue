<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import MarketDataStatus from '@/components/finance/MarketDataStatus.vue';
import { MarketDataLive, fetchRecentImports, type LiveEvent, type LiveEventSource } from '@/services/marketData';
import type { ConnectionState, ImportRunSummary } from '@/types/marketData';

const runs = ref<ImportRunSummary[]>([]);
const loading = ref(true);
const error = ref('');
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');
let requestController: AbortController | undefined;

async function refresh(): Promise<void> {
  requestController?.abort();
  requestController = new AbortController();
  try {
    runs.value = await fetchRecentImports(fetch, requestController.signal);
    error.value = '';
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') return;
    error.value = 'Unable to load market-data status.';
  } finally {
    loading.value = false;
  }
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

const live = new MarketDataLive({
  sourceFactory: browserEventSource,
  onRefresh: (entityType) => {
    if (entityType === 'import_run' || entityType === 'import_item' || entityType === 'quality_finding') void refresh();
  },
  onState: (state) => { connectionState.value = state; },
  reconnectDelayMs: 1_000,
  staleAfterMs: 10_000,
});

const online = () => live.setOnline(true);
const offline = () => live.setOnline(false);

onMounted(() => {
  void refresh();
  live.setOnline(navigator.onLine);
  live.start();
  window.addEventListener('online', online);
  window.addEventListener('offline', offline);
});

onBeforeUnmount(() => {
  requestController?.abort();
  live.stop();
  window.removeEventListener('online', online);
  window.removeEventListener('offline', offline);
});
</script>

<template>
  <div class="markets-view">
    <header class="page-intro">
      <p class="eyebrow">Data foundation</p>
      <h1>Market data</h1>
      <p>Monitor daily imports, quality checks, and safe host-side recovery without exposing provider credentials.</p>
    </header>
    <MarketDataStatus
      :runs="runs"
      :connection-state="connectionState"
      :loading="loading"
      :error="error"
    />
  </div>
</template>
