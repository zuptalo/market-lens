<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import type { SignalContribution } from '@/types/marketData';

const props = defineProps<{ contributions: SignalContribution[] }>();

/**
 * Direction as a word.
 *
 * SC-010: a green bar tells a screen reader nothing, and tells a person who cannot separate red
 * from green the opposite of the truth. So the direction and the magnitude are both text, and
 * colour — where it appears at all — only agrees with what the text already says.
 */
function direction(contribution: SignalContribution): string {
  if (contribution.contribution === null) return 'not available';
  return Number(contribution.contribution) < 0 ? 'lowers' : 'raises';
}

/** Three decimals is what a reader can compare; the stored twelve are for reproducing. */
function magnitude(value: string | null): string {
  if (value === null) return '—';
  return Math.abs(Number(value)).toFixed(3);
}

function signed(value: string | null): string {
  if (value === null) return '—';
  return Number(value).toFixed(3);
}

function weight(value: string): string {
  return Number(value).toFixed(2);
}

const absenceWording: Record<string, string> = {
  insufficient_history: 'not available: too little history for this window',
  feature_unavailable: 'not available: the engine recorded no value',
  composite_undefined: 'not available: the universe composite was undefined',
  liquidity_excluded: 'not available: excluded by the liquidity rule',
  zero_denominator: 'not available: the calculation divided by zero',
};

function unavailable(contribution: SignalContribution): string {
  if (contribution.unavailableReason === null) return '';
  return absenceWording[contribution.unavailableReason]
    ?? `not available: ${contribution.unavailableReason.replace(/_/g, ' ')}`;
}

/** The whole sentence, so a screen reader hears one statement rather than seven cells. */
function statement(contribution: SignalContribution): string {
  if (contribution.contribution === null) {
    return `${contribution.factor}, reading ${contribution.feature}: ${unavailable(contribution)}`;
  }
  return `${contribution.factor}, reading ${contribution.feature} as of `
    + `${contribution.featureSession ?? 'an unrecorded session'}: ${direction(contribution)} the score by `
    + `${magnitude(contribution.contribution)}, at weight ${weight(contribution.weight)}`;
}
</script>

<template>
  <div class="contributions">
    <DataTable
      :value="props.contributions"
      data-testid="contribution-list"
      responsive-layout="stack"
      breakpoint="768px"
      class="contributions__table"
    >
      <Column field="factor" header="Factor" :pt="{ bodyCell: { 'data-label': 'Factor' } }">
        <template #body="{ data }">
          <span class="contributions__factor">{{ data.factor }}</span>
          <span class="contributions__feature">reads {{ data.feature }}</span>
          <span v-if="data.featureSession" class="contributions__session">
            as of {{ data.featureSession }}
          </span>
        </template>
      </Column>
      <Column header="Effect" :pt="{ bodyCell: { 'data-label': 'Effect' } }">
        <template #body="{ data }">
          <span v-if="data.contribution !== null" class="contributions__effect">
            {{ direction(data) }} the score by {{ magnitude(data.contribution) }}
          </span>
          <span v-else class="contributions__absent">{{ unavailable(data) }}</span>
        </template>
      </Column>
      <Column header="Value read" :pt="{ bodyCell: { 'data-label': 'Value read' } }">
        <template #body="{ data }">{{ data.featureValue ?? '—' }}</template>
      </Column>
      <Column header="Factor score" :pt="{ bodyCell: { 'data-label': 'Factor score' } }">
        <template #body="{ data }">{{ signed(data.factorScore) }}</template>
      </Column>
      <Column header="Weight" :pt="{ bodyCell: { 'data-label': 'Weight' } }">
        <template #body="{ data }">{{ weight(data.weight) }}</template>
      </Column>
    </DataTable>

    <!--
      The same information as one sentence per factor, for a reader moving through the page
      linearly. It is not hidden from anybody: a table read cell by cell loses the relationship
      between a factor and what it did, and this restores it.
    -->
    <ul class="contributions__statements">
      <li v-for="item in props.contributions" :key="item.factor">{{ statement(item) }}</li>
    </ul>
  </div>
</template>

<style scoped>
.contributions__factor {
  display: block;
  font-weight: 600;
}

.contributions__feature,
.contributions__session,
.contributions__absent {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
}

.contributions__table {
  --p-datatable-row-padding: 0.6rem;
}

/*
  The sentences are for assistive technology and for anyone who prefers them; the table above
  carries the same content visually. clip rather than display:none, so they stay in the
  accessibility tree.
*/
.contributions__statements {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  margin: 0;
  padding: 0;
}
</style>
