<script setup lang="ts">
import { ref } from 'vue';
import Button from 'primevue/button';
import QualityBadge from './QualityBadge.vue';
import LiveConnectionBadge from './LiveConnectionBadge.vue';
import type { ConnectionState, ImportRunSummary } from '@/types/marketData';

withDefaults(defineProps<{
  runs: ImportRunSummary[];
  connectionState: ConnectionState;
  loading?: boolean;
  error?: string;
}>(), { loading: false, error: '' });

const copyFeedback = ref('');

async function copyRetry(runID: string): Promise<void> {
  const command = `market-lens marketdata retry --run ${runID}`;
  try {
    await navigator.clipboard?.writeText(command);
  } finally {
    copyFeedback.value = 'Retry command copied';
  }
}
</script>

<template>
  <section class="market-data-status" aria-label="Market-data status">
    <div class="status-heading">
      <div>
        <p class="eyebrow">Operations</p>
        <h2>Recent imports</h2>
      </div>
      <LiveConnectionBadge :connection-state="connectionState" />
    </div>

    <p v-if="loading" role="status">Loading market-data status…</p>
    <p v-else-if="error" role="alert" class="status-error">{{ error }}</p>
    <p v-else-if="runs.length === 0" class="empty-state">No market-data imports have run yet.</p>

    <div v-else class="run-list">
      <article v-for="run in runs" :key="run.id" class="run-card">
        <div class="run-summary">
          <div>
            <p class="run-kind">{{ run.kind.replaceAll('_', ' ') }} · {{ run.provider }}</p>
            <time :datetime="run.startedAt">{{ new Date(run.startedAt).toLocaleString() }}</time>
          </div>
          <QualityBadge data-testid="run-status" :status="run.status" />
        </div>
        <ul class="run-counts" aria-label="Import counts">
          <li>Processed <strong>{{ run.counts.processed }}</strong></li>
          <li>Accepted <strong>{{ run.counts.accepted }}</strong></li>
          <li>Rejected <strong>{{ run.counts.rejected }}</strong></li>
          <li>Flagged <strong>{{ run.counts.flagged }}</strong></li>
          <!--
            A run that corrected a session it had already stored is the one worth noticing: every
            feature and every signal derived from that session moved underneath. Stated as text
            with its own label, never by colour, and shown as zero on the ordinary night rather
            than hidden — an absent count and "nothing was corrected" are different claims.
          -->
          <li v-if="run.counts.revised !== undefined">
            Corrected <strong>{{ run.counts.revised }}</strong>
          </li>
        </ul>
        <p v-if="run.errorSummary" class="run-error">{{ run.errorSummary }}</p>
        <div v-if="run.status === 'partial' || run.status === 'failed' || run.status === 'cancelled'" class="retry-row">
          <Button
            data-testid="copy-retry"
            label="Copy host retry command"
            severity="secondary"
            size="small"
            @click="copyRetry(run.id)"
          />
          <span v-if="copyFeedback" role="status">{{ copyFeedback }}</span>
        </div>
      </article>
    </div>
  </section>
</template>
