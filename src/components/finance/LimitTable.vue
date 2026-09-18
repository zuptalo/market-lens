<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Message from 'primevue/message';
import Button from 'primevue/button';
import LoadingBlock from './LoadingBlock.vue';
import type { LimitEvaluation, LimitKind } from '@/types/marketData';

/**
 * Where a person stands against the rules they wrote down.
 *
 * Two rules run through the file. The three states are carried by a word, never by colour alone —
 * somebody who cannot tell red from green must not have to guess whether they are inside their own
 * limit. And nothing here says what would close a gap: the figure, the threshold and the distance
 * between them, and then it stops.
 */

const props = withDefaults(defineProps<{
  limits: LimitEvaluation[];
  currency: string;
  loading?: boolean;
  busy?: boolean;
}>(), { loading: false, busy: false });

defineEmits<{ remove: [kind: LimitKind] }>();

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

const absenceWording: Record<string, string> = {
  portfolio_incomplete: 'a holding could not be priced, so there is no total to measure against',
  nothing_held: 'you are not holding anything yet',
};

function describeKind(kind: LimitKind): string {
  return kindWording[kind] ?? kind.replace(/_/g, ' ');
}

function describeAbsence(reason: string | null): string {
  if (reason === null) return '';
  return absenceWording[reason] ?? reason.replace(/_/g, ' ');
}

function isCount(kind: LimitKind): boolean {
  return kind === 'holding_count';
}

function figure(value: string | null, kind: LimitKind): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  return isCount(kind) ? String(Math.round(parsed)) : `${(parsed * 100).toFixed(1)}%`;
}

function money(value: string): string {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return value;
  return `${parsed.toLocaleString(undefined, { maximumFractionDigits: 0 })} ${props.currency}`;
}

/**
 * The distance between where you are and what you said. Stated as a gap and nothing more: naming an
 * amount to move would be one step from naming a trade, and that is the order-intent feature's job.
 */
function gap(limit: LimitEvaluation): string {
  if (limit.measured === null) return '';
  const measured = Number(limit.measured);
  const threshold = Number(limit.threshold);
  if (!Number.isFinite(measured) || !Number.isFinite(threshold)) return '';
  const difference = measured - threshold;
  if (difference <= 0) return '';
  return isCount(limit.kind)
    ? `${Math.round(difference)} more than you said`
    : `${(difference * 100).toFixed(1)} points over`;
}
</script>

<template>
  <section class="limits" aria-labelledby="limits-heading">
    <h2 id="limits-heading">Where you stand</h2>

    <LoadingBlock v-if="props.loading && props.limits.length === 0" label="Loading your limits…" :rows="3" />

    <Message v-else-if="props.limits.length === 0" severity="info" :closable="false">
      You have not set any limits. This product does not suggest any — a limit is your own rule about
      your own money, and it has no opinion about what yours should be.
    </Message>

    <DataTable
      v-else
      :value="props.limits"
      data-testid="limit-table"
      class="limits__table"
    >
      <Column header="Your rule" :pt="{ bodyCell: { 'data-label': 'Your rule' } }">
        <template #body="{ data }">
          <span class="limits__kind">{{ describeKind(data.kind) }}</span>
          <span class="limits__note">you said {{ figure(data.threshold, data.kind) }}</span>
        </template>
      </Column>
      <Column header="Where you are" :pt="{ bodyCell: { 'data-label': 'Where you are' } }">
        <template #body="{ data }">
          <span v-if="data.measured !== null" class="limits__figure">
            {{ figure(data.measured, data.kind) }}
          </span>
          <span v-else class="limits__absent">—</span>
          <span v-if="data.denominator" class="limits__note">of {{ money(data.denominator) }}</span>
        </template>
      </Column>
      <Column header="State" :pt="{ bodyCell: { 'data-label': 'State' } }">
        <template #body="{ data }">
          <!-- The word carries it. Colour may accompany; it never replaces. -->
          <span class="limits__state" :class="`limits__state--${data.state}`">
            {{ stateWording[data.state] }}
          </span>
          <span v-if="data.state === 'exceeded'" class="limits__note">{{ gap(data) }}</span>
          <span v-else-if="data.state === 'unevaluable'" class="limits__note">
            {{ describeAbsence(data.absenceReason) }}
          </span>
        </template>
      </Column>
      <Column header="What makes it up" :pt="{ bodyCell: { 'data-label': 'What makes it up' } }">
        <template #body="{ data }">
          <ul v-if="data.contributions.length > 0" class="limits__breakdown">
            <li v-for="contribution in data.contributions" :key="contribution.label">
              <span>{{ contribution.label }}</span>
              <span>{{ figure(contribution.share, data.kind === 'holding_count' ? 'sector_share' : data.kind) }}</span>
              <span class="limits__note">{{ money(contribution.value) }}</span>
            </li>
          </ul>
          <span v-else class="limits__absent">—</span>
        </template>
      </Column>
      <Column header="Change" :pt="{ bodyCell: { 'data-label': 'Change' } }">
        <template #body="{ data }">
          <Button
            type="button" size="small" severity="secondary" text label="Remove"
            :disabled="props.busy"
            :aria-label="`Remove your limit on ${describeKind(data.kind).toLowerCase()}`"
            @click="$emit('remove', data.kind)"
          />
        </template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.limits__kind,
.limits__figure,
.limits__state {
  display: block;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.limits__note {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.limits__absent {
  display: block;
  color: var(--p-text-muted-color);
  font-style: italic;
}

/* Colour accompanies the word and never replaces it. Deleting these three rules must leave the
   table exactly as informative, which is what the component test asserts. */
.limits__state--within {
  color: var(--p-green-600);
}

.limits__state--exceeded {
  color: var(--p-red-600);
}

.limits__state--unevaluable {
  color: var(--p-text-muted-color);
}

.limits__breakdown {
  margin: 0;
  padding: 0;
  list-style: none;
}

.limits__breakdown li {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 0 0.75rem;
  font-variant-numeric: tabular-nums;
}

.limits__breakdown li .limits__note {
  grid-column: 1 / -1;
}

.limits__table {
  --p-datatable-row-padding: 0.75rem;
}
</style>
