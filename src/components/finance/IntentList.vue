<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Message from 'primevue/message';
import Button from 'primevue/button';
import LoadingBlock from './LoadingBlock.vue';
import type { IntentConsequence, LimitKind, OrderIntent } from '@/types/marketData';

/**
 * What a person is considering, and what each would do.
 *
 * Two rules run through this file, both inherited from the limits screen and both load-bearing
 * here. A verdict is carried by a word, never by colour alone. And nothing says what to do: the
 * position it would leave, what that would be worth, and what the person's own rules would then
 * say — and then it stops.
 */

const props = withDefaults(defineProps<{
  intents: OrderIntent[];
  currency: string;
  loading?: boolean;
  busy?: boolean;
}>(), { loading: false, busy: false });

defineEmits<{ settle: [id: string, status: 'withdrawn' | 'acted_on'] }>();

const kindWording: Record<LimitKind, string> = {
  instrument_share: 'Most in any one company',
  sector_share: 'Most in any one sector',
  market_share: 'Most in any one market',
  holding_count: 'Most holdings at once',
};

const stateWording: Record<string, string> = {
  within: 'Within',
  exceeded: 'Over',
  unevaluable: 'Not measured',
};

const statusWording: Record<string, string> = {
  considering: 'Considering',
  withdrawn: 'Withdrawn',
  acted_on: 'Acted on',
};

const absenceWording: Record<string, string> = {
  portfolio_incomplete: 'a holding could not be priced, so there is no total to measure against',
  no_price: 'this one could not be priced, so there is no value to state',
};

function describeStatus(status: string): string {
  return statusWording[status] ?? status.replace(/_/g, ' ');
}

function describeAbsence(reason: string | null): string {
  if (reason === null) return '';
  return absenceWording[reason] ?? reason.replace(/_/g, ' ');
}

function quantity(value: string): string {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return value;
  return parsed.toLocaleString(undefined, { maximumFractionDigits: 4 });
}

function money(value: string | null): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return value;
  return `${parsed.toLocaleString(undefined, { maximumFractionDigits: 0 })} ${props.currency}`;
}

function percent(value: string | null): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  return `${(parsed * 100).toFixed(1)}%`;
}

/**
 * A price, written the way money is written.
 *
 * The stored figure carries twelve decimal places so the arithmetic reconciles exactly. Rendering
 * all twelve puts "284.100000000000 SEK" on the screen, which wraps onto two lines on a phone and
 * reads as a fault. Trailing zeroes go; genuine places stay, because a price is not always two.
 */
function price(value: string): string {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return value;
  const trimmed = value.includes('.') ? value.replace(/(\.\d*?[1-9])0+$|\.0+$/, '$1') : value;
  // Two places is the floor, so 284.1 reads as 284.10 rather than as a truncation.
  const places = Math.max(2, (trimmed.split('.')[1] ?? '').length);
  return parsed.toLocaleString(undefined, {
    minimumFractionDigits: 2, maximumFractionDigits: places,
  });
}

function describeIntent(intent: OrderIntent): string {
  const verb = intent.direction === 'buy' ? 'Buy' : 'Sell';
  return `${verb} ${quantity(intent.quantity)} at ${price(intent.price)} ${intent.currency}`;
}

/**
 * A negative resulting position is stated in words as well as with a sign. A minus is easy to miss
 * and easy to read as a rounding artefact; "more than you hold" is neither.
 */
function overdrawn(consequence: IntentConsequence | null): boolean {
  return consequence !== null && Number(consequence.resultingQuantity) < 0;
}
</script>

<template>
  <section class="intents" aria-labelledby="intents-heading">
    <h2 id="intents-heading">What you are considering</h2>

    <LoadingBlock
      v-if="props.loading && props.intents.length === 0"
      label="Loading what you are considering…"
      :rows="3"
    />

    <Message v-else-if="props.intents.length === 0" severity="info" :closable="false">
      You are not considering anything at the moment. Write one down below and this product will
      tell you what it would do — never whether to do it.
    </Message>

    <template v-else>
      <DataTable
        :value="props.intents"
        data-key="id"
        data-testid="intent-list"
        class="intents__table"
      >
        <Column header="Instrument" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
          <template #body="{ data }">
            <span class="intents__figure">{{ data.ticker }}</span>
            <span class="intents__note">{{ data.name }}</span>
          </template>
        </Column>
        <Column header="You are thinking of" :pt="{ bodyCell: { 'data-label': 'You are thinking of' } }">
          <template #body="{ data }">
            <span class="intents__figure">{{ describeIntent(data) }}</span>
          </template>
        </Column>
        <Column
          header="It would leave you holding"
          :pt="{ bodyCell: { 'data-label': 'It would leave you holding' } }"
        >
          <template #body="{ data }">
            <template v-if="data.consequence">
              <span class="intents__figure">{{ quantity(data.consequence.resultingQuantity) }}</span>
              <span v-if="overdrawn(data.consequence)" class="intents__note">
                more than you hold
              </span>
              <span v-else-if="data.consequence.resultingValue" class="intents__note">
                worth {{ money(data.consequence.resultingValue) }}
              </span>
            </template>
            <!-- A settled intent is not evaluated. Showing a zeroed consequence would read as
                 "it would do nothing", which is a different claim entirely. -->
            <span v-else class="intents__absent">no longer evaluated</span>
          </template>
        </Column>
        <Column
          header="Share of your portfolio"
          :pt="{ bodyCell: { 'data-label': 'Share of your portfolio' } }"
        >
          <template #body="{ data }">
            <template v-if="data.consequence && data.consequence.resultingShare">
              <span class="intents__figure">{{ percent(data.consequence.resultingShare) }}</span>
              <span class="intents__note">of {{ money(data.consequence.denominator) }}</span>
            </template>
            <span
              v-else-if="data.consequence && data.consequence.absenceReason"
              class="intents__note"
            >
              Not measured: {{ describeAbsence(data.consequence.absenceReason) }}
            </span>
            <span v-else class="intents__absent">—</span>
          </template>
        </Column>
        <Column header="Your limits afterwards" :pt="{ bodyCell: { 'data-label': 'Your limits afterwards' } }">
          <template #body="{ data }">
            <ul
              v-if="data.consequence && data.consequence.limits.length > 0"
              class="intents__limits"
            >
              <li v-for="limit in data.consequence.limits" :key="limit.kind">
                <span>{{ kindWording[limit.kind as LimitKind] }}</span>
                <!-- The word carries it. Colour may accompany; it never replaces. -->
                <span class="intents__state" :class="`intents__state--${limit.state}`">
                  {{ stateWording[limit.state] }}
                </span>
              </li>
            </ul>
            <span v-else-if="data.consequence" class="intents__absent">you have stated no limits</span>
            <span v-else class="intents__absent">—</span>
          </template>
        </Column>
        <Column header="Status" :pt="{ bodyCell: { 'data-label': 'Status' } }">
          <template #body="{ data }">
            <span class="intents__figure">{{ describeStatus(data.status) }}</span>
            <span v-if="data.status === 'considering'" class="intents__actions">
              <Button
                type="button" size="small" severity="secondary" text label="Withdraw"
                :disabled="props.busy"
                :aria-label="`Withdraw what you wrote down about ${data.ticker}`"
                @click="$emit('settle', data.id, 'withdrawn')"
              />
              <Button
                type="button" size="small" severity="secondary" text label="I acted on this"
                :disabled="props.busy"
                :aria-label="`Record that you acted on what you wrote down about ${data.ticker}`"
                @click="$emit('settle', data.id, 'acted_on')"
              />
            </span>
          </template>
        </Column>
      </DataTable>

      <Message severity="info" :closable="false" class="intents__notice" data-testid="intents-notice">
        Each of these is measured against your portfolio as it stands today, never against another
        one of them. Marking one acted on records that you acted; it does not record a trade — what
        you actually paid belongs in your portfolio, where you enter it yourself.
      </Message>
    </template>
  </section>
</template>

<style scoped>
.intents__figure,
.intents__state {
  display: block;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.intents__note {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.intents__absent {
  display: block;
  color: var(--p-text-muted-color);
  font-style: italic;
}

/* Colour accompanies the word and never replaces it. Deleting these three rules must leave the
   table exactly as informative, which is what the component test asserts. */
.intents__state--within {
  color: var(--p-green-600);
}

.intents__state--exceeded {
  color: var(--p-red-600);
}

.intents__state--unevaluable {
  color: var(--p-text-muted-color);
}

.intents__limits {
  margin: 0;
  padding: 0;
  list-style: none;
}

.intents__limits li {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 0 0.75rem;
}

.intents__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}

.intents__notice {
  margin-top: 1rem;
  max-width: 70ch;
}

.intents__table {
  --p-datatable-row-padding: 0.75rem;
}
</style>
