<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import Message from 'primevue/message';
import Select from 'primevue/select';
import LoadingBlock from '@/components/finance/LoadingBlock.vue';
import HoldingTable from '@/components/finance/HoldingTable.vue';
import TradeEntryForm from '@/components/finance/TradeEntryForm.vue';
import TradeHistory from '@/components/finance/TradeHistory.vue';
import {
  MarketDataLive,
  PortfolioRefusalError,
  correctTrade,
  fetchInstrumentListing,
  fetchPortfolio,
  fetchPortfolioTrades,
  recordTrade,
  setAccountingCurrency,
  withdrawTrade,
  type LiveEventSource,
} from '@/services/marketData';
import { createCoalescer } from '@/services/coalesce';
import { authStore } from '@/stores/auth';
import type {
  ConnectionState,
  Portfolio,
  PortfolioTrade,
  TradeInput,
} from '@/types/marketData';

/**
 * A person's own record of what they hold.
 *
 * Two statements sit above everything else on this screen and are never scrolled past: the product
 * records what you entered and offers no advice, and it cannot tell you your overall return because
 * it does not know what you paid in. The second is a limitation, stated where somebody would
 * otherwise go looking for the number.
 */

const portfolio = ref<Portfolio | null>(null);
const trades = ref<PortfolioTrade[]>([]);
const instruments = ref<{ id: string; ticker: string; name: string }[]>([]);
const editing = ref<PortfolioTrade | null>(null);
const showSuperseded = ref(false);
const loading = ref(true);
const busy = ref(false);
const error = ref('');
const refusal = ref('');
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');

let controller: AbortController | undefined;

const currencies = ['SEK', 'EUR', 'DKK', 'NOK'].map((code) => ({ label: code, value: code }));

const currency = computed({
  get: () => portfolio.value?.accountingCurrency ?? 'EUR',
  set: (value: string) => { void changeCurrency(value); },
});

const returnWording: Record<string, string> = {
  cash_is_not_tracked:
    'This product does not record what you paid in or took out, so it cannot tell you your overall '
    + 'return. What it can tell you is what each holding cost, what it is worth now, and how that '
    + 'compares with its market over the same period.',
};

const returnAbsence = computed(() => {
  const reason = portfolio.value?.totals.returnAbsence ?? '';
  return returnWording[reason] ?? reason;
});

async function load(): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  try {
    const [held, history] = await Promise.all([
      fetchPortfolio(fetch, signal),
      fetchPortfolioTrades({ limit: 200, includeWithdrawn: showSuperseded.value }, fetch, signal),
    ]);
    if (signal.aborted) return;
    portfolio.value = held;
    trades.value = history.items;
    error.value = '';
  } catch {
    if (signal.aborted) return;
    error.value = 'Unable to load your portfolio.';
  } finally {
    if (!signal.aborted) loading.value = false;
  }
}

async function loadInstruments(): Promise<void> {
  try {
    const page = await fetchInstrumentListing({ limit: 200 });
    instruments.value = page.items.map((row) => ({ id: row.id, ticker: row.ticker, name: row.name }));
  } catch {
    // The picker being empty is reported by the form itself; it is not a reason to fail the page.
  }
}

function token(): string {
  return authStore.state.csrfToken ?? '';
}

async function changeCurrency(value: string): Promise<void> {
  busy.value = true;
  try {
    portfolio.value = await setAccountingCurrency(value, token());
    refusal.value = '';
  } catch (caught) {
    refusal.value = caught instanceof PortfolioRefusalError ? caught.refusal.message
      : 'That currency could not be set.';
  } finally {
    busy.value = false;
  }
}

async function submit(input: TradeInput): Promise<void> {
  busy.value = true;
  refusal.value = '';
  try {
    if (editing.value) {
      await correctTrade(editing.value.id, input, token());
      editing.value = null;
    } else {
      await recordTrade(input, token());
    }
    await load();
  } catch (caught) {
    refusal.value = caught instanceof PortfolioRefusalError ? caught.refusal.message
      : 'That could not be recorded.';
  } finally {
    busy.value = false;
  }
}

async function withdraw(trade: PortfolioTrade): Promise<void> {
  busy.value = true;
  refusal.value = '';
  try {
    await withdrawTrade(trade.id, token());
    await load();
  } catch (caught) {
    refusal.value = caught instanceof PortfolioRefusalError ? caught.refusal.message
      : 'That could not be withdrawn.';
  } finally {
    busy.value = false;
  }
}

function toggleSuperseded(value: boolean): void {
  showSuperseded.value = value;
  void load();
}

function money(value: string | null): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  return `${parsed.toLocaleString(undefined, { maximumFractionDigits: 0 })} ${currency.value}`;
}

/**
 * A new stored price changes what a portfolio is worth, and so does the person's own change. Both
 * re-read the whole thing: every figure here is derived, so there is nothing to patch.
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

onMounted(async () => {
  await Promise.all([load(), loadInstruments()]);
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
  <div class="portfolio">
    <header class="page-intro">
      <p class="eyebrow">Your holdings</p>
      <h1>What you own</h1>
      <Message severity="info" :closable="false" class="portfolio__notice" data-testid="no-advice-notice">
        This records what you entered. Nothing here comes from a broker, and nothing here is advice.
      </Message>
    </header>

    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <LoadingBlock v-else-if="loading && portfolio === null" label="Loading your portfolio…" :rows="6" />

    <template v-else-if="portfolio">
      <section class="portfolio__totals" aria-labelledby="totals-heading">
        <h2 id="totals-heading">Altogether</h2>
        <dl class="portfolio__figures" data-testid="portfolio-totals">
          <div>
            <dt>Worth</dt>
            <dd>{{ money(portfolio.totals.value) }}</dd>
          </div>
          <div>
            <dt>Cost</dt>
            <dd>{{ money(portfolio.totals.cost) }}</dd>
          </div>
          <div>
            <dt>Gain or loss, not yet taken</dt>
            <dd>{{ money(portfolio.totals.unrealised) }}</dd>
          </div>
          <div>
            <dt>Gain or loss already taken</dt>
            <dd>
              {{ money(portfolio.totals.realised) }}
              <span>first in, first out</span>
            </dd>
          </div>
        </dl>

        <Message
          v-if="!portfolio.totals.complete"
          severity="warn"
          :closable="false"
          data-testid="incomplete-total"
        >
          {{ portfolio.totals.incompleteReason }}. The holding is still listed below — leaving it out
          would make this total look complete when it is not.
        </Message>

        <!-- Where somebody would go looking for "how am I doing overall". Stated, not missing. -->
        <Message severity="secondary" :closable="false" data-testid="return-absence">
          {{ returnAbsence }}
        </Message>

        <div class="portfolio__currency">
          <label for="portfolio-currency">Show everything in</label>
          <Select
            input-id="portfolio-currency"
            v-model="currency"
            :options="currencies"
            option-label="label"
            option-value="value"
            aria-label="Show everything in"
            :disabled="busy"
          />
        </div>
      </section>

      <HoldingTable
        :holdings="portfolio.holdings"
        :currency="portfolio.accountingCurrency"
        :loading="loading"
      />

      <TradeEntryForm
        :instruments="instruments"
        :editing="editing"
        :busy="busy"
        :refusal="refusal"
        @submit="submit"
        @cancel="editing = null"
      />

      <TradeHistory
        :trades="trades"
        :show-superseded="showSuperseded"
        :busy="busy"
        @edit="editing = $event"
        @withdraw="withdraw"
        @update:show-superseded="toggleSuperseded"
      />
    </template>
  </div>
</template>

<style scoped>
.portfolio__notice {
  margin-top: 0.75rem;
  max-width: 70ch;
}

.portfolio__figures {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 13rem), 1fr));
  gap: 0.75rem 1.5rem;
  margin: 0 0 1rem;
}

.portfolio__figures dt {
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.portfolio__figures dd {
  margin: 0;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.portfolio__figures dd span {
  display: block;
  font-weight: 400;
  font-size: 0.8125rem;
  color: var(--p-text-muted-color);
}

.portfolio__currency {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  margin-top: 1rem;
  max-width: 14rem;
}

.portfolio__currency label {
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.portfolio > section,
.portfolio > form {
  margin-bottom: 2.5rem;
}
</style>
