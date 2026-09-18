<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Message from 'primevue/message';
import Button from 'primevue/button';
import LoadingBlock from './LoadingBlock.vue';
import { formatDate } from '@/utils/datetime';
import type { PaperOrder } from '@/types/marketData';

/**
 * Every order the person promoted, and what became of it.
 *
 * Two rules run through this file, both inherited and both load-bearing. A state is carried by a
 * word, never by colour alone. And nothing says what to do next: what was expected, what was
 * actually paid, and why an order could not fill — then it stops.
 *
 * The price expected sits beside the price paid on purpose. The gap between them is the part of a
 * simulation people forget exists, and it is the whole reason a fill uses a price that did not
 * exist when the order was placed.
 */

const props = withDefaults(defineProps<{
  orders: PaperOrder[];
  currency: string;
  loading?: boolean;
  busy?: boolean;
}>(), { loading: false, busy: false });

defineEmits<{ cancel: [id: string] }>();

const stateWording: Record<string, string> = {
  pending: 'Waiting',
  filled: 'Filled',
  cancelled: 'Withdrawn',
  unfillable: 'Could not fill',
};

const reasonWording: Record<string, string> = {
  insufficient_cash: 'there was not enough cash in the account',
  exceeds_position: 'it was more than the account held',
  no_price: 'no price ever arrived for it',
};

function describeState(state: string): string {
  return stateWording[state] ?? state.replace(/_/g, ' ');
}

function describeReason(reason: string | null): string {
  if (reason === null) return '';
  return reasonWording[reason] ?? reason.replace(/_/g, ' ');
}

/**
 * A price, written the way money is written. The stored figure carries twelve decimal places so the
 * arithmetic reconciles exactly; rendering all twelve reads as a fault.
 */
function price(value: string): string {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return value;
  const trimmed = value.includes('.') ? value.replace(/(\.\d*?[1-9])0+$|\.0+$/, '$1') : value;
  const places = Math.max(2, (trimmed.split('.')[1] ?? '').length);
  return parsed.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: places });
}

function quantity(value: string): string {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return value;
  return parsed.toLocaleString(undefined, { maximumFractionDigits: 4 });
}

function money(value: string): string {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return value;
  return `${parsed.toLocaleString(undefined, { maximumFractionDigits: 0 })} ${props.currency}`;
}

function describeIntent(order: PaperOrder): string {
  const verb = order.direction === 'buy' ? 'Buy' : 'Sell';
  return `${verb} ${quantity(order.quantity)} at ${price(order.expectedPrice)} ${order.currency}`;
}
</script>

<template>
  <section class="paper-orders" aria-labelledby="paper-orders-heading">
    <h2 id="paper-orders-heading">What you promoted</h2>

    <LoadingBlock
      v-if="props.loading && props.orders.length === 0"
      label="Loading your orders…"
      :rows="3"
    />

    <Message v-else-if="props.orders.length === 0" severity="info" :closable="false">
      Nothing promoted yet. An order appears here when you move something you were considering into
      this account; it fills at the open of the next session, which is a price nobody had seen when
      you promoted it.
    </Message>

    <DataTable
      v-else
      :value="props.orders"
      data-key="id"
      data-testid="paper-order-list"
      class="paper-orders__table"
    >
      <Column header="Instrument" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
        <template #body="{ data }">
          <span class="paper-orders__figure">{{ data.ticker }}</span>
          <span class="paper-orders__note">{{ data.name }}</span>
        </template>
      </Column>
      <Column header="You promoted" :pt="{ bodyCell: { 'data-label': 'You promoted' } }">
        <template #body="{ data }">
          <span class="paper-orders__figure">{{ describeIntent(data) }}</span>
          <span class="paper-orders__note">on {{ formatDate(data.placedSession) }}</span>
        </template>
      </Column>
      <Column header="What it actually paid" :pt="{ bodyCell: { 'data-label': 'What it actually paid' } }">
        <template #body="{ data }">
          <template v-if="data.fill">
            <span class="paper-orders__figure">
              {{ price(data.fill.openPrice) }} {{ data.currency }}
            </span>
            <span class="paper-orders__note">
              at the open of {{ formatDate(data.fill.fillSession) }}
            </span>
            <span class="paper-orders__note">
              costs {{ money(data.fill.costs) }}, cash {{ money(data.fill.cashEffect) }}
            </span>
            <!-- Never re-priced; reported. That is what keeps the account reconcilable. -->
            <span v-if="data.fill.barDiverged" class="paper-orders__note">
              The price behind this fill has since been corrected. The fill stands as it was.
            </span>
          </template>
          <span v-else-if="data.state === 'pending'" class="paper-orders__note">
            It will fill at the open of the next session this instrument trades.
          </span>
          <span v-else class="paper-orders__absent">—</span>
        </template>
      </Column>
      <Column header="State" :pt="{ bodyCell: { 'data-label': 'State' } }">
        <template #body="{ data }">
          <!-- The word carries it. Colour may accompany; it never replaces. -->
          <span class="paper-orders__state" :class="`paper-orders__state--${data.state}`">
            {{ describeState(data.state) }}
          </span>
          <span v-if="data.absenceReason" class="paper-orders__note">
            {{ describeReason(data.absenceReason) }}
          </span>
          <span v-if="data.state === 'pending'" class="paper-orders__actions">
            <Button
              type="button" size="small" severity="secondary" text label="Withdraw"
              :disabled="props.busy"
              :aria-label="`Withdraw the promoted order for ${data.ticker}`"
              @click="$emit('cancel', data.id)"
            />
          </span>
        </template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.paper-orders__figure,
.paper-orders__state {
  display: block;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.paper-orders__note {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.paper-orders__absent {
  display: block;
  color: var(--p-text-muted-color);
  font-style: italic;
}

/* Colour accompanies the word and never replaces it. Deleting these four rules must leave the
   table exactly as informative, which is what the component test asserts. */
.paper-orders__state--filled {
  color: var(--p-green-600);
}

.paper-orders__state--unfillable {
  color: var(--p-red-600);
}

.paper-orders__state--pending,
.paper-orders__state--cancelled {
  color: var(--p-text-muted-color);
}

.paper-orders__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}

.paper-orders__table {
  --p-datatable-row-padding: 0.75rem;
}
</style>
