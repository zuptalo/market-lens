<script setup lang="ts">
import Message from 'primevue/message';
import Tag from 'primevue/tag';
import Button from 'primevue/button';
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import LoadingBlock from './LoadingBlock.vue';
import type { QualityFinding } from '@/types/marketData';

const props = withDefaults(defineProps<{
  findings: QualityFinding[];
  loading?: boolean;
  error?: string;
  busy?: boolean;
}>(), { loading: false, error: '', busy: false });

defineEmits<{ accept: [findingID: string] }>();

/**
 * What each rule means for somebody deciding about it.
 *
 * The rule name is the product's vocabulary, not a reader's. Somebody being asked to judge
 * whether a condition is acceptable needs to know what was actually observed.
 */
const explanation: Record<string, string> = {
  provider_gap: 'The source reported a session the exchange calendar does not have.',
  missing_session: 'The source reported nothing for a session the exchange was open for.',
  zero_volume: 'The source reported a session with no volume traded.',
  suspicious_jump: 'The close moved further in one session than the rule allows for.',
  possible_corporate_action_discontinuity: 'The price moved as though a corporate action was applied, and none is recorded.',
  duplicate_source_row: 'The source returned the same session twice.',
  out_of_order_source_row: 'The source returned sessions out of order.',
  invalid_ohlc: 'The open, high, low and close do not agree with each other.',
  non_positive_price: 'The source reported a price of zero or less.',
  negative_volume: 'The source reported negative volume.',
};

function describe(finding: QualityFinding): string {
  return explanation[finding.rule] ?? finding.detail;
}
</script>

<template>
  <section class="quality-findings" aria-labelledby="quality-findings-heading">
    <h2 id="quality-findings-heading">Awaiting your decision</h2>
    <p class="quality-findings__lead">
      Asking the source again did not change these answers, so no further run can settle them.
      Accepting one records that you judged it a limitation of the data — the product does not
      decide that for you.
    </p>

    <Message v-if="props.error" severity="error" :closable="false">{{ props.error }}</Message>

    <LoadingBlock v-else-if="props.loading && props.findings.length === 0" label="Loading findings…" :rows="3" />

    <Message v-else-if="props.findings.length === 0" severity="info" :closable="false">
      Nothing is waiting for you. Every recorded condition has either ended or not yet been
      re-examined.
    </Message>

    <DataTable
      v-else
      :value="props.findings"
      data-testid="quality-finding-list"
      class="quality-findings__table"
    >
      <Column header="Instrument" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
        <template #body="{ data }">
          <span class="quality-findings__ticker">{{ data.ticker ?? data.instrumentId }}</span>
          <span v-if="data.sessionDate" class="quality-findings__session">{{ data.sessionDate }}</span>
        </template>
      </Column>
      <Column header="What was observed" :pt="{ bodyCell: { 'data-label': 'What was observed' } }">
        <template #body="{ data }">
          <span class="quality-findings__rule">{{ describe(data) }}</span>
          <span class="quality-findings__code">{{ data.rule }}</span>
        </template>
      </Column>
      <Column header="Severity" :pt="{ bodyCell: { 'data-label': 'Severity' } }">
        <template #body="{ data }">
          <Tag :severity="data.severity === 'error' ? 'danger' : 'warn'" :value="data.severity" />
        </template>
      </Column>
      <Column header="Decision" :pt="{ bodyCell: { 'data-label': 'Decision' } }">
        <template #body="{ data }">
          <Button
            type="button" size="small" severity="secondary" label="Accept as a limitation"
            :disabled="props.busy"
            :aria-label="`Accept ${data.rule} on ${data.sessionDate ?? 'this instrument'} as a limitation of the data`"
            @click="$emit('accept', data.id)"
          />
        </template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.quality-findings__lead {
  color: var(--p-text-muted-color);
  margin: 0 0 1rem;
  max-width: 70ch;
}

.quality-findings__ticker,
.quality-findings__rule {
  display: block;
  font-weight: 600;
}

.quality-findings__session,
.quality-findings__code {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
}

.quality-findings__table {
  --p-datatable-row-padding: 0.75rem;
}
</style>
