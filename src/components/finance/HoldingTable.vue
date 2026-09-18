<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Message from 'primevue/message';
import LoadingBlock from './LoadingBlock.vue';
import type { PortfolioHolding } from '@/types/marketData';

/**
 * What a person holds, what it is worth, and what it has made or lost.
 *
 * One rule runs through the whole file: a gain and a loss are told apart by a sign and a word, never
 * by colour. Colour may accompany them; it may never carry them. A reader who cannot distinguish red
 * from green would otherwise get the opposite of the truth about their own money — which is a worse
 * failure here than anywhere else in this product.
 */

const props = withDefaults(defineProps<{
  holdings: PortfolioHolding[];
  currency: string;
  loading?: boolean;
  error?: string;
}>(), { loading: false, error: '' });

function money(value: string | null, currency: string): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  return `${parsed.toLocaleString(undefined, { maximumFractionDigits: 2 })} ${currency}`;
}

function quantity(value: string): string {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed.toLocaleString(undefined, { maximumFractionDigits: 6 }) : value;
}

function percent(value: string | null): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  return `${(parsed * 100).toFixed(2)}%`;
}

/** The word that carries the direction, so the sign is never the only thing saying it. */
function direction(value: string | null): string {
  if (value === null) return '';
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed === 0) return 'level';
  return parsed > 0 ? 'up' : 'down';
}

const valuationWording: Record<string, string> = {
  no_price: 'no stored price for this instrument',
  no_rate: 'no stored exchange rate for that session',
};

const comparisonWording: Record<string, string> = {
  series_does_not_cover_window: 'the market series does not reach back to when you bought',
  no_benchmark_for_market: 'this market has no benchmark series',
  holding_not_valued: 'the holding itself could not be valued',
};

function describeValuation(reason: string): string {
  return valuationWording[reason] ?? reason.replace(/_/g, ' ');
}

function describeComparison(reason: string): string {
  return comparisonWording[reason] ?? reason.replace(/_/g, ' ');
}
</script>

<template>
  <section class="holdings" aria-labelledby="holdings-heading">
    <h2 id="holdings-heading">What you hold</h2>

    <Message v-if="props.error" severity="error" :closable="false">{{ props.error }}</Message>

    <LoadingBlock v-else-if="props.loading && props.holdings.length === 0" label="Loading your holdings…" :rows="4" />

    <Message v-else-if="props.holdings.length === 0" severity="info" :closable="false">
      You have not recorded anything yet. Record a purchase and it will appear here with what it is
      worth at the latest stored price.
    </Message>

    <DataTable
      v-else
      :value="props.holdings"
      data-testid="holding-table"
      class="holdings__table"
    >
      <Column header="Instrument" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
        <template #body="{ data }">
          <span class="holdings__ticker">{{ data.ticker }}</span>
          <span class="holdings__name">{{ data.name }}</span>
        </template>
      </Column>
      <Column header="Held" :pt="{ bodyCell: { 'data-label': 'Held' } }">
        <template #body="{ data }">
          <span class="holdings__figure">{{ quantity(data.quantity) }}</span>
        </template>
      </Column>
      <Column header="Cost" :pt="{ bodyCell: { 'data-label': 'Cost' } }">
        <template #body="{ data }">
          <span class="holdings__figure">{{ money(data.cost, data.currency) }}</span>
          <span class="holdings__note">first in, first out</span>
        </template>
      </Column>
      <Column header="Worth" :pt="{ bodyCell: { 'data-label': 'Worth' } }">
        <template #body="{ data }">
          <template v-if="data.valuation.value !== null">
            <span class="holdings__figure">{{ money(data.valuation.value, props.currency) }}</span>
            <span class="holdings__note">at the close of {{ data.valuation.session }}</span>
            <span v-if="data.valuation.conversionRate" class="holdings__note">
              converted at {{ data.valuation.conversionRate }}
            </span>
          </template>
          <span v-else class="holdings__absent">
            Not valued — {{ describeValuation(data.valuation.absenceReason ?? '') }}
          </span>
        </template>
      </Column>
      <Column header="Gain or loss" :pt="{ bodyCell: { 'data-label': 'Gain or loss' } }">
        <template #body="{ data }">
          <template v-if="data.unrealised !== null">
            <!-- The word first, so the direction survives without colour and without the sign. -->
            <span class="holdings__figure" :class="`holdings__figure--${direction(data.unrealised)}`">
              {{ direction(data.unrealised) === 'down' ? 'Down' : direction(data.unrealised) === 'up' ? 'Up' : 'Level' }}
              {{ money(data.unrealised, props.currency) }}
            </span>
          </template>
          <span v-else class="holdings__absent">—</span>
        </template>
      </Column>
      <Column header="Against the market" :pt="{ bodyCell: { 'data-label': 'Against the market' } }">
        <template #body="{ data }">
          <template v-if="data.comparison.absenceReason === null">
            <span class="holdings__figure">{{ percent(data.comparison.holdingReturn) }}</span>
            <span class="holdings__note">
              {{ data.comparison.series }} {{ percent(data.comparison.benchmarkReturn) }}
              since {{ data.comparison.fromSession }}
            </span>
          </template>
          <span v-else class="holdings__absent">
            Not compared — {{ describeComparison(data.comparison.absenceReason) }}
          </span>
        </template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.holdings__ticker,
.holdings__figure {
  display: block;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.holdings__name,
.holdings__note {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.holdings__absent {
  display: block;
  color: var(--p-text-muted-color);
  font-style: italic;
}

/* Colour accompanies the word; it never replaces it. Removing these two rules must leave the table
   exactly as informative, which is what the component test asserts. */
.holdings__figure--up {
  color: var(--p-green-600);
}

.holdings__figure--down {
  color: var(--p-red-600);
}

.holdings__table {
  --p-datatable-row-padding: 0.75rem;
}
</style>
