<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import InstrumentIdentity from '@/components/finance/InstrumentIdentity.vue';
import { fetchDailyPrices, fetchInstrument } from '@/services/marketData';
import type { InstrumentDetail, PricePage } from '@/types/marketData';

const route = useRoute();
const detail = ref<InstrumentDetail | null>(null);
const prices = ref<PricePage>({ items: [], nextCursor: null });
const loading = ref(true);
const error = ref('');
const controller = new AbortController();
const returnQuery = typeof route.query.return === 'string' ? route.query.return : '';
const backURL = `/markets${returnQuery ? `?${returnQuery}` : ''}`;
onMounted(async () => {
  try {
    const id = String(route.params.instrumentId);
    [detail.value, prices.value] = await Promise.all([fetchInstrument(id, fetch, controller.signal), fetchDailyPrices(id, { limit: 200 }, fetch, controller.signal)]);
  } catch (cause) {
    if (!(cause instanceof DOMException && cause.name === 'AbortError')) error.value = 'Unable to load instrument market data.';
  } finally { loading.value = false; }
});
onBeforeUnmount(() => controller.abort());
</script>

<template>
  <div class="instrument-detail-view">
    <a :href="backURL" class="back-link">← Back to instruments</a>
    <p v-if="loading" role="status">Loading instrument market data…</p>
    <p v-else-if="error" role="alert" class="status-error">{{ error }}</p>
    <InstrumentIdentity v-else-if="detail" :instrument="detail" :latest-bar="detail.latestBar" :history="detail.history" :quality-summary="detail.qualitySummary" />
    <section v-if="detail && prices.items.length === 0" aria-label="Daily price history"><p class="empty-state">No additional daily history rows are available.</p></section>
  </div>
</template>
