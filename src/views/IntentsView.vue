<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import Message from 'primevue/message';
import LoadingBlock from '@/components/finance/LoadingBlock.vue';
import IntentList from '@/components/finance/IntentList.vue';
import IntentForm from '@/components/finance/IntentForm.vue';
import {
  MarketDataLive,
  PortfolioRefusalError,
  fetchInstrumentListing,
  fetchOrderIntents,
  fetchPortfolio,
  recordOrderIntent,
  settleOrderIntent,
  type LiveEventSource,
} from '@/services/marketData';
import { createCoalescer } from '@/services/coalesce';
import { authStore } from '@/stores/auth';
import type { ConnectionState, IntentInput, IntentReport, IntentStatus } from '@/types/marketData';

/**
 * What a person is considering, and what each would do.
 *
 * This is the closest this product comes to telling somebody what to do, which is why the
 * authorship runs the other way: the person writes down the intent and the product evaluates it.
 * Nothing on this screen proposes one, and the statement at the top says so.
 */

const report = ref<IntentReport | null>(null);
const instruments = ref<{ id: string; ticker: string; name: string }[]>([]);
const instrumentsError = ref('');
const currency = ref('');
const loading = ref(true);
const busy = ref(false);
const error = ref('');
const refusal = ref('');
const includeSettled = ref(false);
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');

let controller: AbortController | undefined;

const intents = computed(() => report.value?.intents ?? []);

async function load(): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  try {
    report.value = await fetchOrderIntents(includeSettled.value, fetch, signal);
    error.value = '';
  } catch {
    if (signal.aborted) return;
    error.value = 'Unable to load what you are considering.';
  } finally {
    if (!signal.aborted) loading.value = false;
  }
}

/**
 * The accounting currency belongs to the portfolio, and the choices belong to the instrument
 * listing. Both are read here rather than duplicated: a screen that carried its own copy of either
 * would eventually disagree with the screen that owns it.
 */
async function loadContext(): Promise<void> {
  const [holdings, listing] = await Promise.allSettled([
    fetchPortfolio(fetch, controller?.signal),
    fetchInstrumentListing({ status: 'active', limit: 500 }, fetch, controller?.signal),
  ]);
  if (holdings.status === 'fulfilled') currency.value = holdings.value.accountingCurrency;
  if (listing.status === 'fulfilled') {
    instruments.value = listing.value.items.map((item) => ({
      id: item.id, ticker: item.ticker, name: item.name,
    }));
    instrumentsError.value = instruments.value.length > 0 ? ''
      : 'This product carries no instruments yet, so there is nothing to consider.';
  } else {
    instrumentsError.value = 'Unable to load the list of instruments.';
  }
}

function token(): string {
  return authStore.state.csrfToken ?? '';
}

async function record(input: IntentInput): Promise<void> {
  busy.value = true;
  refusal.value = '';
  try {
    report.value = await recordOrderIntent(input, token());
    error.value = '';
  } catch (caught) {
    refusal.value = caught instanceof PortfolioRefusalError ? caught.refusal.message
      : 'That could not be written down.';
  } finally {
    busy.value = false;
  }
}

/**
 * Withdrawing or marking one acted on. Marking acted on records that the person acted; it posts no
 * trade, because what they actually paid is a fact only they can assert.
 */
async function settle(id: string, status: Exclude<IntentStatus, 'considering'>): Promise<void> {
  busy.value = true;
  refusal.value = '';
  try {
    report.value = await settleOrderIntent(id, status, token());
    error.value = '';
  } catch (caught) {
    refusal.value = caught instanceof PortfolioRefusalError ? caught.refusal.message
      : 'That could not be recorded.';
  } finally {
    busy.value = false;
  }
}

/**
 * A new stored price moves every consequence, and so does a recorded trade or a changed limit. All
 * three already publish their own events; the screen re-reads on any of them rather than the
 * product publishing a fourth that would say the same thing.
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
  await loadContext();
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
  <div class="intents-view">
    <header class="page-intro">
      <p class="eyebrow">Order intents</p>
      <h1>What you are considering</h1>
      <Message severity="info" :closable="false" class="intents-view__notice" data-testid="no-advice-notice">
        You write these down; this product does not propose any. It tells you what each one would do
        to your holdings and to the limits you set yourself, and offers no advice about whether to
        act. Nothing is sent anywhere — there is no broker connection here, and marking one acted on
        does not record a trade.
      </Message>
    </header>

    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <template v-else>
      <LoadingBlock v-if="loading && report === null" label="Loading what you are considering…" :rows="3" />

      <template v-else>
        <IntentList
          :intents="intents"
          :currency="currency"
          :loading="loading"
          :busy="busy"
          @settle="settle"
        />

        <IntentForm
          :instruments="instruments"
          :instruments-error="instrumentsError"
          :busy="busy"
          :refusal="refusal"
          @submit="record"
        />
      </template>
    </template>
  </div>
</template>

<style scoped>
.intents-view__notice {
  margin-top: 0.75rem;
  max-width: 70ch;
}

.intents-view > section,
.intents-view > form,
.intents-view :deep(section),
.intents-view :deep(form) {
  margin-bottom: 2.5rem;
}
</style>
