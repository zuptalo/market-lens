<script setup lang="ts">
import Message from 'primevue/message';
import Tag from 'primevue/tag';
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import type { FeatureRunSummary } from '@/types/marketData';

const props = withDefaults(defineProps<{
  runs: FeatureRunSummary[];
  loading?: boolean;
  error?: string;
}>(), { loading: false, error: '' });

/** A run's outcome, in the vocabulary the engine itself uses. */
function severity(status: FeatureRunSummary['status']): 'success' | 'warn' | 'danger' | 'info' {
  if (status === 'succeeded') return 'success';
  if (status === 'partial') return 'warn';
  if (status === 'failed') return 'danger';
  return 'info';
}

function whatItCovered(run: FeatureRunSummary): string {
  if (run.kind === 'definition') return run.definitionName ?? 'one definition';
  if (run.kind === 'incremental') return 'what one import changed';
  return 'every instrument';
}

function counted(value: number): string {
  return new Intl.NumberFormat('en-GB').format(value);
}

function elapsed(run: FeatureRunSummary): string {
  if (!run.finishedAt) return 'running';
  const seconds = (Date.parse(run.finishedAt) - Date.parse(run.startedAt)) / 1000;
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds < 90) return `${seconds.toFixed(1)}s`;
  return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`;
}

/**
 * How many instruments a run left with older values than it intended. A partial run is not a
 * failure of the store — the previous values stand — but nothing on the market screens can
 * say so, which is why it is stated here.
 */
function staleAfter(run: FeatureRunSummary): string {
  if (run.failedCount === 0) return '';
  return `${counted(run.failedCount)} instrument${run.failedCount === 1 ? '' : 's'} kept their earlier values`;
}
</script>

<template>
  <section class="feature-runs" aria-labelledby="feature-runs-heading">
    <h2 id="feature-runs-heading">Feature computation</h2>
    <p class="feature-runs__lead">
      The engine derives the statistics the market screens show. A run that fails or ends
      partial leaves the earlier values in place, where they read as current.
    </p>

    <Message v-if="props.error" severity="error" :closable="false">{{ props.error }}</Message>

    <Message v-else-if="!props.loading && props.runs.length === 0" severity="info" :closable="false">
      The feature engine has not run in this deployment. Until it does, the statistics on the
      market screens are absent rather than current.
    </Message>

    <DataTable
      v-else
      :value="props.runs"
      :loading="props.loading"
      data-testid="feature-run-list"
      responsive-layout="stack"
      breakpoint="768px"
      class="feature-runs__table"
    >
      <Column field="kind" header="Run" :pt="{ bodyCell: { 'data-label': 'Run' } }">
        <template #body="{ data }">
          <span class="feature-runs__kind">{{ data.kind }}</span>
          <span class="feature-runs__covered">{{ whatItCovered(data) }}</span>
        </template>
      </Column>
      <Column field="status" header="Outcome" :pt="{ bodyCell: { 'data-label': 'Outcome' } }">
        <template #body="{ data }">
          <Tag :severity="severity(data.status)" :value="data.status" />
          <span v-if="staleAfter(data)" class="feature-runs__stale">{{ staleAfter(data) }}</span>
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
      <Column header="Values" :pt="{ bodyCell: { 'data-label': 'Values' } }">
        <template #body="{ data }">{{ counted(data.valueCount) }}</template>
      </Column>
      <Column header="Version" :pt="{ bodyCell: { 'data-label': 'Version' } }">
        <template #body="{ data }">{{ data.appVersion ?? '—' }}</template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.feature-runs__lead {
  color: var(--p-text-muted-color);
  margin: 0 0 1rem;
  max-width: 60ch;
}

.feature-runs__kind {
  display: block;
  font-weight: 600;
  text-transform: capitalize;
}

.feature-runs__covered,
.feature-runs__stale {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
}

.feature-runs__table {
  --p-datatable-row-padding: 0.75rem;
}
</style>
