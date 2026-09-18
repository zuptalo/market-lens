<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import Button from 'primevue/button';
import InputNumber from 'primevue/inputnumber';
import Message from 'primevue/message';
import Select from 'primevue/select';
import LoadingBlock from '@/components/finance/LoadingBlock.vue';
import PaperAccountSummary from '@/components/finance/PaperAccountSummary.vue';
import PaperOrderList from '@/components/finance/PaperOrderList.vue';
import PaperPromoteForm from '@/components/finance/PaperPromoteForm.vue';
import {
  MarketDataLive,
  PortfolioRefusalError,
  cancelPaperOrder,
  fetchOrderIntents,
  fetchPaperAccount,
  openPaperAccount,
  promotePaperOrder,
  type LiveEventSource,
} from '@/services/marketData';
import { createCoalescer } from '@/services/coalesce';
import { authStore } from '@/stores/auth';
import type { ConnectionState, OrderIntent, PaperAccount } from '@/types/marketData';

/**
 * A simulated account over stored prices.
 *
 * It answers the question nothing else here can: would the decisions somebody actually made have
 * worked? And it answers it forward, on prices nobody had seen when the order was placed.
 *
 * This screen reads **no real portfolio at all**, deliberately. Feature 022 records what a person
 * did and this records what they would have done; putting both on one screen is one careless
 * addition away from a number that means nothing.
 */

const account = ref<PaperAccount | null>(null);
const intents = ref<OrderIntent[]>([]);
const opened = ref(false);
const loading = ref(true);
const busy = ref(false);
const error = ref('');
const refusal = ref('');
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');

const startingCash = ref<number | null>(null);
const accountingCurrency = ref('SEK');
const currencies = ['SEK', 'EUR', 'NOK', 'DKK'].map((code) => ({ label: code, value: code }));

let controller: AbortController | undefined;

const orders = computed(() => account.value?.orders ?? []);
const currency = computed(() => account.value?.accountingCurrency ?? accountingCurrency.value);
const considering = computed(() => intents.value.filter((intent) => intent.status === 'considering'));

async function load(): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  try {
    account.value = await fetchPaperAccount(fetch, signal);
    opened.value = account.value !== null;
    error.value = '';
  } catch {
    if (signal.aborted) return;
    error.value = 'Unable to load your paper account.';
    return;
  } finally {
    if (!signal.aborted) loading.value = false;
  }

  // The intents are what can be promoted. Their failure does not take the account down with it:
  // the account is what this screen is for.
  try {
    intents.value = (await fetchOrderIntents(false, fetch, signal)).intents;
  } catch {
    intents.value = [];
  }
}

function token(): string {
  return authStore.state.csrfToken ?? '';
}

async function open(): Promise<void> {
  if (startingCash.value === null || startingCash.value <= 0) return;
  busy.value = true;
  refusal.value = '';
  try {
    account.value = await openPaperAccount(String(startingCash.value), accountingCurrency.value, token());
    opened.value = true;
    error.value = '';
    await load();
  } catch (caught) {
    refusal.value = caught instanceof PortfolioRefusalError ? caught.refusal.message
      : 'That account could not be opened.';
  } finally {
    busy.value = false;
  }
}

async function promote(intentId: string): Promise<void> {
  busy.value = true;
  refusal.value = '';
  try {
    account.value = await promotePaperOrder(intentId, token());
    error.value = '';
    await load();
  } catch (caught) {
    refusal.value = caught instanceof PortfolioRefusalError ? caught.refusal.message
      : 'That could not be promoted.';
  } finally {
    busy.value = false;
  }
}

async function cancel(orderId: string): Promise<void> {
  busy.value = true;
  refusal.value = '';
  try {
    account.value = await cancelPaperOrder(orderId, token());
    error.value = '';
  } catch (caught) {
    refusal.value = caught instanceof PortfolioRefusalError ? caught.refusal.message
      : 'That order could not be withdrawn.';
  } finally {
    busy.value = false;
  }
}

/**
 * Orders fill on the server after an import, with nobody watching — which is the whole reason the
 * pass is scheduled rather than derived on read. So this screen listens for the account's own event
 * as well as for new prices.
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
  <div class="paper-view">
    <header class="page-intro">
      <p class="eyebrow">Paper trading</p>
      <h1>What your decisions would have done</h1>
      <Message severity="info" :closable="false" class="paper-view__notice" data-testid="paper-notice">
        A simulation over stored prices. Nothing here was traded and no order was placed anywhere.
        Orders come from intents you wrote down yourself — this product proposes none — and each
        fills at the open of the session after you promoted it, which is a price nobody had seen at
        the time.
      </Message>
    </header>

    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <template v-else>
      <LoadingBlock v-if="loading && account === null && !opened" label="Loading your account…" :rows="4" />

      <!--
        No account yet. Opening it is the only moment its terms can be chosen, so the form says so
        rather than letting somebody discover it afterwards.
      -->
      <form v-else-if="account === null" class="paper-open" aria-labelledby="open-heading" @submit.prevent="open">
        <h2 id="open-heading">Open a paper account</h2>
        <p class="paper-open__lead">
          Choose what it starts with and what currency it reports in. Both are fixed once it is
          open and cannot be changed afterwards — a record that can be retuned once the result is
          known is not a record of anything.
        </p>

        <Message v-if="refusal" severity="warn" :closable="false" data-testid="open-refusal">
          {{ refusal }}
        </Message>

        <div class="paper-open__field">
          <label for="paper-cash">It starts with</label>
          <InputNumber input-id="paper-cash" v-model="startingCash" :min="0" :max-fraction-digits="2" />
        </div>
        <div class="paper-open__field">
          <label for="paper-currency">Reported in</label>
          <Select
            input-id="paper-currency"
            v-model="accountingCurrency"
            :options="currencies"
            option-label="label"
            option-value="value"
            aria-label="The currency the paper account reports in"
          />
        </div>
        <Button
          type="submit"
          label="Open it"
          :disabled="startingCash === null || startingCash <= 0"
          :loading="busy"
        />
      </form>

      <template v-else>
        <PaperAccountSummary :account="account" />

        <PaperOrderList
          :orders="orders"
          :currency="currency"
          :loading="loading"
          :busy="busy"
          @cancel="cancel"
        />

        <PaperPromoteForm
          :intents="considering"
          :busy="busy"
          :refusal="refusal"
          @promote="promote"
        />
      </template>
    </template>
  </div>
</template>

<style scoped>
.paper-view__notice {
  margin-top: 0.75rem;
  max-width: 70ch;
}

.paper-view > section,
.paper-view > form,
.paper-view :deep(section),
.paper-view :deep(form) {
  margin-bottom: 2.5rem;
}

.paper-open {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  max-width: 34rem;
}

.paper-open__lead {
  color: var(--p-text-muted-color);
  margin: 0;
  max-width: 60ch;
}

.paper-open__field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.paper-open__field label {
  font-size: 0.875rem;
  font-weight: 600;
}
</style>
