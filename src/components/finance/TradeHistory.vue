<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Message from 'primevue/message';
import Tag from 'primevue/tag';
import Button from 'primevue/button';
import ToggleSwitch from 'primevue/toggleswitch';
import type { PortfolioTrade } from '@/types/marketData';

/**
 * Everything the person recorded, including the versions that no longer count.
 *
 * Superseded and withdrawn entries stay visible because that is what makes a portfolio
 * reconcilable: somebody checking last month's figure against a broker statement needs the version
 * that produced it. Hiding them would make the history tidier and the product less trustworthy.
 */

const props = withDefaults(defineProps<{
  trades: PortfolioTrade[];
  showSuperseded?: boolean;
  busy?: boolean;
}>(), { showSuperseded: false, busy: false });

defineEmits<{
  edit: [trade: PortfolioTrade];
  withdraw: [trade: PortfolioTrade];
  'update:showSuperseded': [value: boolean];
}>();

function amount(value: string, currency: string): string {
  const parsed = Number(value);
  return Number.isFinite(parsed)
    ? `${parsed.toLocaleString(undefined, { maximumFractionDigits: 2 })} ${currency}`
    : value;
}

function quantity(value: string): string {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed.toLocaleString(undefined, { maximumFractionDigits: 6 }) : value;
}

const statusLabel: Record<string, string> = {
  current: 'Counts',
  superseded: 'Replaced',
  withdrawn: 'Withdrawn',
};
</script>

<template>
  <section class="history" aria-labelledby="history-heading">
    <header class="history__header">
      <h2 id="history-heading">What you recorded</h2>
      <label class="history__toggle">
        <ToggleSwitch
          :model-value="props.showSuperseded"
          @update:model-value="$emit('update:showSuperseded', $event)"
        />
        <span>Show replaced and withdrawn entries</span>
      </label>
    </header>

    <Message v-if="props.trades.length === 0" severity="info" :closable="false">
      Nothing recorded yet.
    </Message>

    <DataTable
      v-else
      :value="props.trades"
      data-testid="trade-history"
      class="history__table"
    >
      <Column header="Instrument" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
        <template #body="{ data }">
          <span class="history__ticker">{{ data.ticker }}</span>
          <span class="history__note">entry {{ data.sequence }}</span>
        </template>
      </Column>
      <Column header="Action" :pt="{ bodyCell: { 'data-label': 'Action' } }">
        <template #body="{ data }">
          <!-- The word carries it; the tag's colour only repeats it. -->
          <Tag :severity="data.direction === 'buy' ? 'info' : 'secondary'"
               :value="data.direction === 'buy' ? 'Bought' : 'Sold'" />
        </template>
      </Column>
      <Column header="Shares" :pt="{ bodyCell: { 'data-label': 'Shares' } }">
        <template #body="{ data }">
          <span class="history__figure">{{ quantity(data.quantity) }}</span>
        </template>
      </Column>
      <Column header="Price" :pt="{ bodyCell: { 'data-label': 'Price' } }">
        <template #body="{ data }">
          <span class="history__figure">{{ amount(data.price, data.currency) }}</span>
          <span v-if="Number(data.costs) > 0" class="history__note">
            plus {{ amount(data.costs, data.currency) }} in costs
          </span>
        </template>
      </Column>
      <Column header="Date" :pt="{ bodyCell: { 'data-label': 'Date' } }">
        <template #body="{ data }">
          <span class="history__figure">{{ data.tradeDate }}</span>
          <span v-if="data.changedAt" class="history__note">corrected</span>
        </template>
      </Column>
      <Column header="Status" :pt="{ bodyCell: { 'data-label': 'Status' } }">
        <template #body="{ data }">
          <span class="history__status">{{ statusLabel[data.status] }}</span>
        </template>
      </Column>
      <Column header="Change" :pt="{ bodyCell: { 'data-label': 'Change' } }">
        <template #body="{ data }">
          <div v-if="data.status === 'current'" class="history__actions">
            <Button
              type="button" size="small" severity="secondary" text label="Correct"
              :disabled="props.busy"
              :aria-label="`Correct the ${data.direction} of ${data.ticker} on ${data.tradeDate}`"
              @click="$emit('edit', data)"
            />
            <Button
              type="button" size="small" severity="secondary" text label="Withdraw"
              :disabled="props.busy"
              :aria-label="`Withdraw the ${data.direction} of ${data.ticker} on ${data.tradeDate}`"
              @click="$emit('withdraw', data)"
            />
          </div>
          <span v-else class="history__note">kept for reference</span>
        </template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.history__header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.history__toggle {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.875rem;
  color: var(--p-text-muted-color);
}

.history__ticker,
.history__figure,
.history__status {
  display: block;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.history__note {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.history__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}

.history__table {
  --p-datatable-row-padding: 0.75rem;
}
</style>
