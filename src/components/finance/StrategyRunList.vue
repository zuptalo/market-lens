<script setup lang="ts">
import Message from 'primevue/message';
import Tag from 'primevue/tag';
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import type { StrategyRunSummary } from '@/types/marketData';

const props = withDefaults(defineProps<{
  runs: StrategyRunSummary[];
  loading?: boolean;
  error?: string;
}>(), { loading: false, error: '' });

function severity(status: StrategyRunSummary['status']): 'success' | 'warn' | 'danger' | 'info' {
  if (status === 'succeeded') return 'success';
  if (status === 'partial') return 'warn';
  if (status === 'failed') return 'danger';
  return 'info';
}

function whatItCovered(run: StrategyRunSummary): string {
  if (run.kind === 'strategy') return 'one published version, whole history';
  if (run.kind === 'incremental') return 'the sessions a feature run changed';
  return 'every instrument, whole history';
}

function counted(value: number): string {
  return new Intl.NumberFormat('en-GB').format(value);
}

function elapsed(run: StrategyRunSummary): string {
  if (!run.finishedAt) return 'running';
  const seconds = (Date.parse(run.finishedAt) - Date.parse(run.startedAt)) / 1000;
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds < 90) return `${seconds.toFixed(1)}s`;
  return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`;
}

/**
 * A partial run left some instruments with the views a previous run recorded. That is correct
 * behaviour and completely invisible on the screens that show signals, which is exactly why it
 * has to be stated here.
 */
function keptEarlier(run: StrategyRunSummary): string {
  if (run.failedCount === 0) return '';
  return `${counted(run.failedCount)} instrument${run.failedCount === 1 ? '' : 's'} kept their earlier signals`;
}
</script>

<template>
  <section class="strategy-runs" aria-labelledby="strategy-runs-heading">
    <h2 id="strategy-runs-heading">Strategy computation</h2>
    <p class="strategy-runs__lead">
      Strategies score the features once the engine has computed them. A run that ends partial
      leaves the earlier signals in place, where they read as current.
    </p>

    <Message v-if="props.error" severity="error" :closable="false">{{ props.error }}</Message>

    <Message v-else-if="!props.loading && props.runs.length === 0" severity="info" :closable="false">
      No strategy has run in this deployment. Until one does, the instrument and ranking screens
      have no view to show rather than a stale one.
    </Message>

    <DataTable
      v-else
      :value="props.runs"
      :loading="props.loading"
      data-testid="strategy-run-list"
      responsive-layout="stack"
      breakpoint="768px"
      class="strategy-runs__table"
    >
      <Column field="kind" header="Run" :pt="{ bodyCell: { 'data-label': 'Run' } }">
        <template #body="{ data }">
          <span class="strategy-runs__kind">{{ data.kind }}</span>
          <span class="strategy-runs__covered">{{ whatItCovered(data) }}</span>
        </template>
      </Column>
      <Column field="status" header="Outcome" :pt="{ bodyCell: { 'data-label': 'Outcome' } }">
        <template #body="{ data }">
          <Tag :severity="severity(data.status)" :value="data.status" />
          <span v-if="keptEarlier(data)" class="strategy-runs__stale">{{ keptEarlier(data) }}</span>
        </template>
      </Column>
      <Column field="startedAt" header="Started" :pt="{ bodyCell: { 'data-label': 'Started' } }">
        <template #body="{ data }">
          <time :datetime="data.startedAt">{{ new Date(data.startedAt).toLocaleString() }}</time>
        </template>
      </Column>
      <Column header="Took" :pt="{ bodyCell: { 'data-label': 'Took' } }">
        <template #body="{ data }">{{ elapsed(data) }}</template>
      </Column>
      <Column header="Instruments" :pt="{ bodyCell: { 'data-label': 'Instruments' } }">
        <template #body="{ data }">{{ counted(data.instrumentCount) }}</template>
      </Column>
      <Column header="Signals" :pt="{ bodyCell: { 'data-label': 'Signals' } }">
        <template #body="{ data }">{{ counted(data.signalCount) }}</template>
      </Column>
      <Column header="Version" :pt="{ bodyCell: { 'data-label': 'Version' } }">
        <template #body="{ data }">{{ data.appVersion ?? '—' }}</template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.strategy-runs__lead {
  color: var(--p-text-muted-color);
  margin: 0 0 1rem;
  max-width: 60ch;
}

.strategy-runs__kind {
  display: block;
  font-weight: 600;
  text-transform: capitalize;
}

.strategy-runs__covered,
.strategy-runs__stale {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
}

.strategy-runs__table {
  --p-datatable-row-padding: 0.75rem;
}
</style>
