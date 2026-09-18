<script setup lang="ts">
import { computed } from 'vue';
import Message from 'primevue/message';
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import { formatDate } from '@/utils/datetime';
import type { PaperAccount } from '@/types/marketData';

/**
 * What the simulated account is worth, and what it has done.
 *
 * The statement at the top is the one this whole screen is built around. A simulated account that
 * grows looks exactly like a recommendation to do the same with real money, and this is the one
 * screen in the product where that confusion would cost somebody something.
 *
 * It is also the only screen that reports a **return**, and it says why it may: the real portfolio
 * declines to, because it never saw the deposits and withdrawals; here the account started at a
 * stated balance and this product recorded every movement since. Without that sentence the two
 * screens read as an inconsistency.
 */

const props = defineProps<{ account: PaperAccount }>();

const currency = computed(() => props.account.accountingCurrency);

function money(value: string | null): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return value;
  return `${parsed.toLocaleString(undefined, { maximumFractionDigits: 0 })} ${currency.value}`;
}

function percent(value: string | null): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  return `${(parsed * 100).toFixed(1)}%`;
}

function quantity(value: string): string {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed.toLocaleString(undefined, { maximumFractionDigits: 4 }) : value;
}

const incompleteWording: Record<string, string> = {
  no_price: 'one of its holdings could not be priced',
  no_rate: 'no stored exchange rate covers one of its holdings',
};

const comparisonWording: Record<string, string> = {
  no_benchmark: 'this market has no benchmark stored',
  holding_not_valued: 'the holding could not be priced',
  benchmark_incomplete: 'the benchmark does not cover the whole period',
};

function describeIncomplete(reason: string | null): string {
  if (reason === null) return '';
  return incompleteWording[reason] ?? reason.replace(/_/g, ' ');
}

function describeComparisonAbsence(reason: string | null): string {
  if (reason === null) return '';
  return comparisonWording[reason] ?? reason.replace(/_/g, ' ');
}

/** Gain and loss carry a word as well as a sign, because colour alone is what a colour-blind
 * reader loses here. */
function movement(value: string | null): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  const word = parsed >= 0 ? 'Up' : 'Down';
  return `${word} ${money(String(Math.abs(parsed)))}`;
}
</script>

<template>
  <section class="paper-summary" aria-labelledby="paper-summary-heading">
    <h2 id="paper-summary-heading">How it has done</h2>

    <Message severity="info" :closable="false" class="paper-summary__notice" data-testid="simulation-notice">
      This is a simulation over stored prices. Nothing here was traded, no order was placed
      anywhere, and no figure here belongs beside one from your real portfolio. It charges the
      trading costs stated below and fills at the open of the session after each order was
      promoted — a price nobody had seen at the time.
    </Message>

    <dl class="paper-summary__figures">
      <div>
        <dt>Cash</dt>
        <dd>{{ money(props.account.cash) }}</dd>
      </div>
      <div>
        <dt>Holdings</dt>
        <dd>{{ money(props.account.totals.value) }}</dd>
      </div>
      <div>
        <dt>Started with</dt>
        <dd>
          {{ money(props.account.startingCash) }}
          <small>on {{ formatDate(props.account.openedAt) }}</small>
        </dd>
      </div>
      <div>
        <dt>Return</dt>
        <dd>
          <template v-if="props.account.totals.totalReturn !== null">
            {{ percent(props.account.totals.totalReturn) }}
            <!-- The one return figure in the product, and the sentence that makes it consistent
                 with the real portfolio declining to state one. -->
            <small>
              This account can state a return because it started at a stated balance and every
              movement since was recorded here. Your real portfolio cannot, because it never saw
              what you paid in or took out.
            </small>
          </template>
          <template v-else>
            <span class="paper-summary__absent">not stated</span>
            <small>{{ describeIncomplete(props.account.totals.incompleteReason) }} — so the
              total would be understated rather than unknown, which is a worse thing to show.</small>
          </template>
        </dd>
      </div>
      <div>
        <dt>Gain or loss already taken</dt>
        <dd>{{ movement(props.account.totals.realised) }}</dd>
      </div>
      <div>
        <dt>Trading costs charged</dt>
        <dd>
          <small>
            Brokerage {{ props.account.costs.brokerageBps }} bp, minimum
            {{ props.account.costs.brokerageMinimum }}; slippage
            {{ props.account.costs.slippageBps }} bp; currency spread
            {{ props.account.costs.currencySpreadBps }} bp.
          </small>
        </dd>
      </div>
    </dl>

    <Message
      v-if="props.account.holdings.length === 0"
      severity="info"
      :closable="false"
      class="paper-summary__notice"
    >
      The account is holding nothing at the moment.
    </Message>

    <DataTable
      v-else
      :value="props.account.holdings"
      data-key="instrumentId"
      data-testid="paper-holdings"
      class="paper-summary__table"
    >
      <Column header="Instrument" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
        <template #body="{ data }">
          <span class="paper-summary__figure">{{ data.ticker }}</span>
          <span class="paper-summary__note">{{ data.name }}</span>
        </template>
      </Column>
      <Column header="Held" :pt="{ bodyCell: { 'data-label': 'Held' } }">
        <template #body="{ data }">
          <span class="paper-summary__figure">{{ quantity(data.quantity) }}</span>
        </template>
      </Column>
      <Column header="Cost" :pt="{ bodyCell: { 'data-label': 'Cost' } }">
        <template #body="{ data }">
          <span class="paper-summary__figure">{{ money(data.cost) }}</span>
        </template>
      </Column>
      <Column header="Worth" :pt="{ bodyCell: { 'data-label': 'Worth' } }">
        <template #body="{ data }">
          <template v-if="data.value">
            <span class="paper-summary__figure">{{ money(data.value) }}</span>
            <span class="paper-summary__note">at the close of {{ formatDate(data.session) }}</span>
          </template>
          <span v-else class="paper-summary__absent">could not be priced</span>
        </template>
      </Column>
      <Column header="Gain or loss" :pt="{ bodyCell: { 'data-label': 'Gain or loss' } }">
        <template #body="{ data }">
          <span class="paper-summary__figure">{{ movement(data.unrealised) }}</span>
        </template>
      </Column>
      <Column header="Against the market" :pt="{ bodyCell: { 'data-label': 'Against the market' } }">
        <template #body="{ data }">
          <template v-if="data.comparison.holdingReturn && data.comparison.benchmarkReturn">
            <span class="paper-summary__figure">{{ percent(data.comparison.holdingReturn) }}</span>
            <span class="paper-summary__note">
              {{ data.comparison.series }} {{ percent(data.comparison.benchmarkReturn) }} over the
              same period
            </span>
          </template>
          <span v-else class="paper-summary__absent">
            {{ describeComparisonAbsence(data.comparison.absenceReason) || '—' }}
          </span>
        </template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.paper-summary__notice {
  max-width: 70ch;
}

.paper-summary__figures {
  display: grid;
  gap: 1rem 2rem;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 14rem), 1fr));
  margin: 1.5rem 0;
}

.paper-summary__figures dt {
  font-size: 0.8rem;
  font-weight: 600;
  letter-spacing: 0.02em;
  text-transform: uppercase;
  opacity: 0.6;
  margin: 0 0 0.15rem;
}

.paper-summary__figures dd {
  margin: 0;
  font-size: 1.25rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.paper-summary__figures dd small {
  display: block;
  font-size: 0.8125rem;
  font-weight: 400;
  color: var(--p-text-muted-color);
  max-width: 46ch;
}

.paper-summary__figure {
  display: block;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.paper-summary__note {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.paper-summary__absent {
  display: block;
  color: var(--p-text-muted-color);
  font-style: italic;
}

.paper-summary__table {
  --p-datatable-row-padding: 0.75rem;
}
</style>
