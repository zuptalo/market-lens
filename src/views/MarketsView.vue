<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import MarketDataStatus from '@/components/finance/MarketDataStatus.vue';
import { InstrumentSearchClient, MarketDataLive, fetchRecentImports, type LiveEvent, type LiveEventSource } from '@/services/marketData';
import type { ConnectionState, ImportRunSummary, InstrumentPage } from '@/types/marketData';

const initial = new URLSearchParams(window.location.search);
const query = ref(initial.get('q') ?? '');
const mic = ref(initial.get('exchange') ?? '');
const country = ref(initial.get('country') ?? '');
const currency = ref(initial.get('currency') ?? '');
const active = ref(initial.get('active') ?? 'true');
const instruments = ref<InstrumentPage>({ items: [], nextCursor: null });
const instrumentLoading = ref(true);
const instrumentError = ref('');
const runs = ref<ImportRunSummary[]>([]);
const loading = ref(true);
const error = ref('');
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');
let requestController: AbortController | undefined;

const searchClient = new InstrumentSearchClient(fetch, (page) => {
  instruments.value = page;
  instrumentLoading.value = false;
  instrumentError.value = '';
});
const currentQuery = computed(() => {
  const params = new URLSearchParams();
  if (query.value) params.set('q', query.value);
  if (mic.value) params.set('exchange', mic.value);
  if (country.value) params.set('country', country.value);
  if (currency.value) params.set('currency', currency.value);
  if (active.value) params.set('active', active.value);
  return params.toString();
});

async function search(): Promise<void> {
  instrumentLoading.value = true;
  instrumentError.value = '';
  window.history.replaceState(window.history.state, '', `/markets${currentQuery.value ? `?${currentQuery.value}` : ''}`);
  try {
    await searchClient.search({ query: query.value, mic: mic.value, country: country.value, currency: currency.value,
      active: active.value === '' ? undefined : active.value === 'true', limit: 50 });
  } catch {
    instrumentLoading.value = false;
    instrumentError.value = 'Unable to load instruments. Your filters have been preserved.';
  }
}
watch([query, mic, country, currency, active], () => { void search(); });

async function refresh(): Promise<void> {
  requestController?.abort();
  requestController = new AbortController();
  try {
    runs.value = await fetchRecentImports(fetch, requestController.signal);
    error.value = '';
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') return;
    error.value = 'Unable to load market-data status.';
  } finally { loading.value = false; }
}

function browserEventSource(url: string, lastEventId: string): LiveEventSource {
  const endpoint = lastEventId ? `${url}?last_event_id=${encodeURIComponent(lastEventId)}` : url;
  const source = new EventSource(endpoint);
  return { addEventListener(type, listener) { source.addEventListener(type, (event) => {
    const message = event as MessageEvent<string>;
    listener({ lastEventId: message.lastEventId ?? '', type, data: message.data ?? '' } satisfies LiveEvent);
  }); }, close: () => source.close() };
}

const live = new MarketDataLive({ sourceFactory: browserEventSource, onRefresh: (entityType) => {
  if (entityType === 'import_run' || entityType === 'import_item' || entityType === 'quality_finding') void refresh();
  if (entityType === 'daily_bar' || entityType === 'instrument') void search();
}, onState: (state) => { connectionState.value = state; }, reconnectDelayMs: 1_000, staleAfterMs: 10_000 });
const online = () => live.setOnline(true);
const offline = () => live.setOnline(false);

onMounted(() => {
  void search(); void refresh(); live.setOnline(navigator.onLine); live.start();
  window.addEventListener('online', online); window.addEventListener('offline', offline);
});
onBeforeUnmount(() => {
  searchClient.cancel(); requestController?.abort(); live.stop();
  window.removeEventListener('online', online); window.removeEventListener('offline', offline);
});
</script>

<template>
  <div class="markets-view">
    <header class="page-intro"><p class="eyebrow">Data foundation</p><h1>Market data</h1><p>Search exchange-qualified instruments and inspect stored daily history without implying live quotes.</p></header>
    <section class="instrument-browser" aria-labelledby="instrument-browser-heading">
      <div class="status-heading"><div><p class="eyebrow">Curated universe</p><h2 id="instrument-browser-heading">Instruments</h2></div></div>
      <div class="instrument-filters">
        <label>Search<input v-model="query" type="search" aria-label="Search instruments" placeholder="Ticker, company, or ISIN"></label>
        <label>Exchange<select v-model="mic" aria-label="Exchange"><option value="">All exchanges</option><option v-for="value in ['XSTO','XCSE','XHEL','XOSL']" :key="value">{{ value }}</option></select></label>
        <label>Country<select v-model="country" aria-label="Country"><option value="">All countries</option><option v-for="value in ['SE','DK','FI','NO']" :key="value">{{ value }}</option></select></label>
        <label>Currency<select v-model="currency" aria-label="Currency"><option value="">All currencies</option><option v-for="value in ['SEK','DKK','EUR','NOK']" :key="value">{{ value }}</option></select></label>
        <label>Active status<select v-model="active" aria-label="Active status"><option value="">All statuses</option><option value="true">Active</option><option value="false">Inactive</option></select></label>
      </div>
      <p v-if="instrumentLoading" role="status">Loading instruments…</p>
      <p v-else-if="instrumentError" role="alert" class="status-error">{{ instrumentError }}</p>
      <p v-else-if="instruments.items.length === 0" role="status" class="empty-state">No instruments match these filters.</p>
      <ul v-else class="instrument-results" aria-label="Instrument results">
        <li v-for="instrument in instruments.items" :key="instrument.id"><a :href="`/markets/${instrument.id}?return=${encodeURIComponent(currentQuery)}`" :aria-label="`${instrument.name}, ${instrument.ticker}, ${instrument.exchange.mic}`"><strong>{{ instrument.name }}</strong><span>{{ instrument.ticker }} · {{ instrument.exchange.mic }}</span><span>{{ instrument.currency }} · {{ instrument.isin }}</span></a></li>
      </ul>
    </section>
    <MarketDataStatus :runs="runs" :connection-state="connectionState" :loading="loading" :error="error" />
  </div>
</template>
