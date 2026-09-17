<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import Message from 'primevue/message';
import LoadingBlock from '@/components/finance/LoadingBlock.vue';
import LimitTable from '@/components/finance/LimitTable.vue';
import LimitForm from '@/components/finance/LimitForm.vue';
import {
  MarketDataLive,
  PortfolioRefusalError,
  fetchRiskLimits,
  removeRiskLimit,
  setRiskLimit,
  type LiveEventSource,
} from '@/services/marketData';
import { createCoalescer } from '@/services/coalesce';
import { authStore } from '@/stores/auth';
import type { ConnectionState, LimitKind, RiskReport } from '@/types/marketData';

/**
 * A person's own limits, and where they stand against them.
 *
 * The statement at the top is the one this whole screen is built around: these are your rules, the
 * product sets none and suggests none. Everything below is arithmetic on them.
 */

const report = ref<RiskReport | null>(null);
const loading = ref(true);
const busy = ref(false);
const error = ref('');
const refusal = ref('');
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');

let controller: AbortController | undefined;

const limits = computed(() => report.value?.limits ?? []);
const currency = computed(() => report.value?.accountingCurrency ?? '');

async function load(): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  try {
    report.value = await fetchRiskLimits(fetch, signal);
    error.value = '';
  } catch {
    if (signal.aborted) return;
    error.value = 'Unable to load your limits.';
  } finally {
    if (!signal.aborted) loading.value = false;
  }
}

function token(): string {
  return authStore.state.csrfToken ?? '';
}

async function state(kind: LimitKind, threshold: string): Promise<void> {
  busy.value = true;
  refusal.value = '';
  try {
    report.value = await setRiskLimit(kind, threshold, token());
  } catch (caught) {
    refusal.value = caught instanceof PortfolioRefusalError ? caught.refusal.message
      : 'That limit could not be set.';
  } finally {
    busy.value = false;
  }
}

async function remove(kind: LimitKind): Promise<void> {
  busy.value = true;
  refusal.value = '';
  try {
    report.value = await removeRiskLimit(kind, token());
  } catch {
    refusal.value = 'That limit could not be removed.';
  } finally {
    busy.value = false;
  }
}

/**
 * A new stored price moves every percentage, and so does a recorded trade. Both already publish
 * their own events; the screen re-reads on either rather than the product publishing a third that
 * would say the same thing.
 */
const refresh = createCoalescer(async () => { await load(); });

function browserEventSource(url: string, lastEventId: string): LiveEventSource {
  const endpoint = lastEventId ? `${url}?last_event_id=${encodeURIComponent(lastEventId)}` : url;
  return new EventSource(endpoint, { withCredentials: true }) as unknown as LiveEventSource;
}

const live = new MarketDataLive({
  sourceFactory: browserEventSource,
  onRefresh: (entityType) => { refresh.add(entityType); },
  onState: (value) => { connectionState.value = value; },
  reconnectDelayMs: 1_000,
  staleAfterMs: 10_000,
});
const online = () => live.setOnline(true);
const offline = () => live.setOnline(false);

onMounted(async () => {
  await load();
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
  <div class="risk">
    <header class="page-intro">
      <p class="eyebrow">Your limits</p>
      <h1>The rules you set yourself</h1>
      <Message severity="info" :closable="false" class="risk__notice" data-testid="own-rules-notice">
        These are your own limits. This product sets none, suggests none, and offers no advice — it
        tells you where you stand against what you wrote down, and shows the arithmetic.
      </Message>
    </header>

    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <template v-else>
      <LoadingBlock v-if="loading && report === null" label="Loading your limits…" :rows="4" />

      <template v-else>
        <LimitTable
          :limits="limits"
          :currency="currency"
          :loading="loading"
          :busy="busy"
          @remove="remove"
        />

        <LimitForm :busy="busy" :refusal="refusal" @submit="state" />
      </template>
    </template>
  </div>
</template>

<style scoped>
.risk__notice {
  margin-top: 0.75rem;
  max-width: 70ch;
}

.risk > section,
.risk > form,
.risk :deep(section),
.risk :deep(form) {
  margin-bottom: 2.5rem;
}
</style>
