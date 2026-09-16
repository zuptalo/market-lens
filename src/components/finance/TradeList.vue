<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Message from 'primevue/message';
import Tag from 'primevue/tag';
import Button from 'primevue/button';
import LoadingBlock from './LoadingBlock.vue';
import type { BacktestTrade } from '@/types/marketData';

/**
 * What the simulation did, and why.
 *
 * Every row leads to the signal that caused it. That link is the difference between a list of
 * trades and an explanation: a reader who disagrees with a purchase can follow it to the
 * strategy's own per-factor contributions instead of being asked to take the trade on trust.
 */

const props = withDefaults(defineProps<{
  trades: BacktestTrade[];
  loading?: boolean;
  error?: string;
  hasMore?: boolean;
  busy?: boolean;
}>(), { loading: false, error: '', hasMore: false, busy: false });

defineEmits<{ more: [] }>();

function amount(value: string, currency: string): string {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  return `${parsed.toLocaleString(undefined, { maximumFractionDigits: 2 })} ${currency}`;
}

function quantity(value: string): string {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed.toLocaleString() : value;
}

/** Costs read as one figure because a reader compares them with the trade, not with each other. */
function costs(trade: BacktestTrade): string {
  const total = Number(trade.brokerage) + Number(trade.slippage) + Number(trade.currencySpread);
  return Number.isFinite(total) ? total.toLocaleString(undefined, { maximumFractionDigits: 2 }) : '—';
}

function signalLink(trade: BacktestTrade): string {
  const [, session] = trade.signalId.split('/');
  return `/instruments/${trade.instrumentId}?as_of=${session ?? trade.signalSession}`;
}
</script>

<template>
  <section class="trades" aria-labelledby="trades-heading">
    <h3 id="trades-heading">What it did</h3>

    <Message v-if="props.error" severity="error" :closable="false">{{ props.error }}</Message>

    <LoadingBlock v-else-if="props.loading && props.trades.length === 0" label="Loading trades…" :rows="4" />

    <Message v-else-if="props.trades.length === 0" severity="info" :closable="false">
      This configuration made no trade over the range. That is a result: the rules were applied and
      never asked for anything to change hands.
    </Message>

    <template v-else>
      <DataTable
        :value="props.trades"
        data-testid="trade-list"
        responsive-layout="stack"
        breakpoint="768px"
        class="trades__table"
      >
        <Column header="Instrument" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
          <template #body="{ data }">
            <span class="trades__ticker">{{ data.ticker }}</span>
            <span class="trades__name">{{ data.name }}</span>
          </template>
        </Column>
        <Column header="Action" :pt="{ bodyCell: { 'data-label': 'Action' } }">
          <template #body="{ data }">
            <!-- The word carries the direction. Colour only repeats it. -->
            <Tag :severity="data.direction === 'buy' ? 'info' : 'secondary'" :value="data.direction === 'buy' ? 'Bought' : 'Sold'" />
          </template>
        </Column>
        <Column header="Executed" :pt="{ bodyCell: { 'data-label': 'Executed' } }">
          <template #body="{ data }">
            <span class="trades__session">{{ data.executionSession }}</span>
            <span class="trades__signal">on the signal of {{ data.signalSession }}</span>
          </template>
        </Column>
        <Column header="Quantity" :pt="{ bodyCell: { 'data-label': 'Quantity' } }">
          <template #body="{ data }">
            <span class="trades__figure">{{ quantity(data.quantity) }}</span>
          </template>
        </Column>
        <Column header="Price" :pt="{ bodyCell: { 'data-label': 'Price' } }">
          <template #body="{ data }">
            <span class="trades__figure">{{ amount(data.price, data.currency) }}</span>
            <span v-if="data.conversionRate" class="trades__rate">at {{ data.conversionRate }} to the euro</span>
          </template>
        </Column>
        <Column header="Cost" :pt="{ bodyCell: { 'data-label': 'Cost' } }">
          <template #body="{ data }">
            <span class="trades__figure">{{ costs(data) }}</span>
          </template>
        </Column>
        <Column header="Why" :pt="{ bodyCell: { 'data-label': 'Why' } }">
          <template #body="{ data }">
            <router-link
              class="trades__reason"
              :to="signalLink(data)"
              :aria-label="`See why the strategy scored ${data.ticker} on ${data.signalSession}`"
            >
              See the signal
            </router-link>
          </template>
        </Column>
      </DataTable>

      <Button
        v-if="props.hasMore"
        type="button"
        severity="secondary"
        label="Show more trades"
        :loading="props.busy"
        class="trades__more"
        @click="$emit('more')"
      />
    </template>
  </section>
</template>

<style scoped>
.trades__ticker,
.trades__session,
.trades__figure {
  display: block;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.trades__name,
.trades__signal,
.trades__rate {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.trades__table {
  --p-datatable-row-padding: 0.75rem;
}

.trades__more {
  margin-top: 1rem;
}
</style>
