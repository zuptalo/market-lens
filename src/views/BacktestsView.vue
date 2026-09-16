<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import Message from 'primevue/message';
import Select from 'primevue/select';
import Tag from 'primevue/tag';
import LoadingBlock from '@/components/finance/LoadingBlock.vue';
import EquityCurve from '@/components/finance/EquityCurve.vue';
import MeasureTable from '@/components/finance/MeasureTable.vue';
import TradeList from '@/components/finance/TradeList.vue';
import {
  MarketDataLive,
  fetchBacktest,
  fetchBacktestEquity,
  fetchBacktestTrades,
  fetchBacktests,
  type LiveEventSource,
} from '@/services/marketData';
import { createCoalescer } from '@/services/coalesce';
import type {
  BacktestDetail,
  BacktestEquityCurve,
  BacktestSummary,
  BacktestTrade,
  ConnectionState,
} from '@/types/marketData';

/**
 * What a strategy would have done.
 *
 * The screen is arranged around one claim and its qualifications. The result is stated, then what
 * it is being compared against, then what it actually did and why — and the statement that it is
 * a simulation over past data sits above all of it rather than below, because a caveat a reader
 * has to scroll to is a caveat that was not made.
 */

const runs = ref<BacktestSummary[]>([]);
const selected = ref<string>('');
const detail = ref<BacktestDetail | null>(null);
const curve = ref<BacktestEquityCurve>({ currency: '', items: [] });
const trades = ref<BacktestTrade[]>([]);
const nextCursor = ref<string | null>(null);
const loadingRuns = ref(true);
const loadingResult = ref(false);
const loadingTrades = ref(false);
const loadingMore = ref(false);
const error = ref('');
const tradeError = ref('');
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');

let controller: AbortController | undefined;

const options = computed(() => runs.value.map((run) => ({
  value: run.id,
  label: `${run.configuration.title} — ${run.fromSession} to ${run.toSession}`,
})));

const configuration = computed(() => detail.value?.configuration ?? null);

async function loadRuns(): Promise<void> {
  loadingRuns.value = true;
  try {
    const items = await fetchBacktests();
    runs.value = items;
    error.value = '';
    if (items.length > 0 && !items.some((run) => run.id === selected.value)) {
      selected.value = items[0].id;
    }
  } catch {
    error.value = 'Unable to load the recorded backtests.';
  } finally {
    loadingRuns.value = false;
  }
}

async function loadResult(id: string): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loadingResult.value = true;
  loadingTrades.value = true;
  try {
    const [result, equity, page] = await Promise.all([
      fetchBacktest(id, fetch, signal),
      fetchBacktestEquity(id, fetch, signal),
      fetchBacktestTrades(id, { limit: 50 }, fetch, signal),
    ]);
    if (signal.aborted) return;
    detail.value = result;
    curve.value = equity;
    trades.value = page.items;
    nextCursor.value = page.nextCursor;
    error.value = '';
    tradeError.value = '';
  } catch {
    if (signal.aborted) return;
    error.value = 'Unable to load this backtest.';
  } finally {
    if (!signal.aborted) {
      loadingResult.value = false;
      loadingTrades.value = false;
    }
  }
}

async function loadMoreTrades(): Promise<void> {
  if (!nextCursor.value || !selected.value) return;
  loadingMore.value = true;
  try {
    const page = await fetchBacktestTrades(selected.value, { cursor: nextCursor.value, limit: 50 });
    trades.value = [...trades.value, ...page.items];
    nextCursor.value = page.nextCursor;
  } catch {
    tradeError.value = 'Unable to load more trades.';
  } finally {
    loadingMore.value = false;
  }
}

/** A completed backtest arrives as backtest.completed.v1. The list is re-read; the open result
 *  is not disturbed, because a reader studying one result should not have it replaced underneath
 *  them by another one finishing. */
const refresh = createCoalescer(async () => { await loadRuns(); });

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

watch(selected, (id) => { if (id) void loadResult(id); });

onMounted(async () => {
  await loadRuns();
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

function money(value: string, currency: string): string {
  const parsed = Number(value);
  return Number.isFinite(parsed)
    ? `${parsed.toLocaleString(undefined, { maximumFractionDigits: 0 })} ${currency}`
    : value;
}

const skipWording: Record<string, string> = {
  not_selected: 'not among the highest scored',
  already_held: 'already held at the weight the rule asks for',
  no_cash: 'not enough cash left to buy a whole share',
  not_executable: 'the instrument did not trade again in time',
  no_price: 'no price on the session it would have executed',
  no_rate: 'no stored exchange rate for that session',
  no_next_session: 'no session left in the range to execute on',
};

function describeSkip(reason: string): string {
  return skipWording[reason] ?? reason.replace(/_/g, ' ');
}
</script>

<template>
  <div class="backtests">
    <header class="page-intro">
      <p class="eyebrow">Backtesting</p>
      <h1>What a strategy would have done</h1>
      <!-- The statement sits above the result, not beneath it. A caveat a reader has to scroll
           to is a caveat that was not made. -->
      <Message severity="warn" :closable="false" class="backtests__caveat" data-testid="simulation-notice">
        This is a simulation over past data. It is not a prediction, not advice, and not evidence
        that the same rules will work again.
      </Message>
    </header>

    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <LoadingBlock v-else-if="loadingRuns && runs.length === 0" label="Loading backtests…" :rows="4" />

    <Message v-else-if="runs.length === 0" severity="info" :closable="false">
      No backtest has been run in this deployment. A backtest is started from the command line by
      the owner — nothing here can start one.
    </Message>

    <template v-else>
      <div class="backtests__picker">
        <label for="backtest-select">Result</label>
        <Select
          id="backtest-select"
          v-model="selected"
          :options="options"
          option-label="label"
          option-value="value"
          class="backtests__select"
        />
      </div>

      <LoadingBlock v-if="loadingResult && !detail" label="Loading the result…" :rows="8" />

      <template v-else-if="detail && configuration">
        <section class="backtests__configuration" aria-labelledby="configuration-heading">
          <h2 id="configuration-heading">{{ configuration.title }}</h2>
          <p class="backtests__intent">{{ configuration.intent }}</p>
          <p class="backtests__caveat-text">{{ configuration.caveat }}</p>
          <dl class="backtests__rules" data-testid="configuration-rules">
            <div>
              <dt>Strategy</dt>
              <dd>{{ configuration.strategy.name }} v{{ configuration.strategy.version }}</dd>
            </div>
            <div>
              <dt>Universe</dt>
              <dd>{{ configuration.universe }}</dd>
            </div>
            <div>
              <dt>Range</dt>
              <dd>{{ detail.fromSession }} to {{ detail.toSession }}</dd>
            </div>
            <div>
              <dt>Starting capital</dt>
              <dd>{{ money(configuration.startingCapital, configuration.accountingCurrency) }}</dd>
            </div>
            <div>
              <dt>Holdings</dt>
              <dd>{{ configuration.sizing.holdings }}, equally weighted</dd>
            </div>
            <div>
              <dt>Rebalanced</dt>
              <dd>{{ configuration.rebalance.schedule }} ({{ detail.rebalanceCount }} times)</dd>
            </div>
            <div>
              <dt>Costs</dt>
              <dd>
                {{ configuration.costs.brokerageBps }} bp brokerage, minimum
                {{ configuration.costs.brokerageMinimum }};
                {{ configuration.costs.slippageBps }} bp slippage;
                {{ configuration.costs.currencySpreadBps }} bp currency spread
              </dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd><Tag :severity="detail.status === 'succeeded' ? 'success' : 'danger'" :value="detail.status" /></dd>
            </div>
          </dl>
        </section>

        <MeasureTable
          :measures="detail.measures"
          :benchmarks="detail.benchmarks"
          :currency="configuration.accountingCurrency"
        />

        <EquityCurve :points="curve.items" :currency="curve.currency" />

        <TradeList
          :trades="trades"
          :loading="loadingTrades"
          :error="tradeError"
          :has-more="nextCursor !== null"
          :busy="loadingMore"
          @more="loadMoreTrades"
        />

        <section v-if="detail.skipped.length > 0" class="backtests__skipped" aria-labelledby="skipped-heading">
          <h3 id="skipped-heading">What it considered and did not do</h3>
          <p class="backtests__skipped-lead">
            Every signal the schedule looked at either produced a trade or recorded why it did not.
            Signals on sessions the schedule does not trade on were never considered.
          </p>
          <ul class="backtests__skipped-list" data-testid="skipped-reasons">
            <li v-for="tally in detail.skipped" :key="tally.reason">
              <strong>{{ tally.count.toLocaleString() }}</strong> {{ describeSkip(tally.reason) }}
            </li>
          </ul>
        </section>
      </template>
    </template>
  </div>
</template>

<style scoped>
.backtests__caveat {
  margin-top: 0.75rem;
  max-width: 70ch;
}

.backtests__picker {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  margin-bottom: 1.5rem;
  max-width: 34rem;
}

.backtests__picker label {
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.backtests__select {
  width: 100%;
}

.backtests__intent,
.backtests__caveat-text {
  max-width: 70ch;
  color: var(--p-text-muted-color);
  margin: 0 0 0.75rem;
}

.backtests__rules {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 14rem), 1fr));
  gap: 0.75rem 1.5rem;
  margin: 0 0 2rem;
}

.backtests__rules dt {
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.backtests__rules dd {
  margin: 0;
  font-weight: 600;
}

.backtests__skipped {
  margin-top: 2rem;
}

.backtests__skipped-lead {
  color: var(--p-text-muted-color);
  max-width: 70ch;
}

.backtests__skipped-list {
  margin: 0;
  padding-left: 1.25rem;
}

.backtests__skipped-list li {
  margin-bottom: 0.25rem;
}

.backtests > section {
  margin-bottom: 2.5rem;
}
</style>
