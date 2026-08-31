<script setup lang="ts">
import { computed } from 'vue';
import { formatDecimal } from '@/utils/decimal';
import type { CorporateAction, QualityFinding } from '@/types/marketData';

/**
 * The context that keeps a price series from being misleading.
 *
 * The chart marks these sessions, but a marker on a canvas is not a readable fact: it cannot
 * be reached by keyboard, it says nothing to a screen reader, and its label appears on hover
 * — which is exactly what the specification forbids relying on (FR-015, SC-008). So every
 * annotation also appears here, in a list, always visible.
 *
 * This is what lets a reader tell a real fifty-percent move from an unadjusted split.
 */

const props = withDefaults(defineProps<{
  actions: CorporateAction[];
  findings: QualityFinding[];
  missingSessions: string[];
}>(), { missingSessions: () => [] });

function actionDetail(action: CorporateAction): string {
  // A two-for-one split is a ratio of 2, not 2.00, so ratios keep no forced decimals while
  // a dividend is money and does.
  if (action.ratio) return `ratio ${formatDecimal(action.ratio, 0)}`;
  if (action.amount) return `${formatDecimal(action.amount)}${action.currency ? ` ${action.currency}` : ''}`;
  if (action.oldSymbol || action.newSymbol) return `${action.oldSymbol ?? '—'} → ${action.newSymbol ?? '—'}`;
  return '';
}

const hasAnything = computed(() =>
  props.actions.length > 0 || props.findings.length > 0 || props.missingSessions.length > 0);
</script>

<template>
  <section class="annotations" aria-labelledby="annotations-heading">
    <h2 id="annotations-heading">What would otherwise distort this series</h2>

    <p v-if="!hasAnything" class="empty-state">
      No corporate actions, open quality findings, or missing sessions in this window.
    </p>

    <template v-else>
      <section v-if="props.actions.length" aria-labelledby="actions-heading">
        <h3 id="actions-heading">Corporate actions</h3>
        <ul class="annotation-list">
          <li v-for="action in props.actions" :key="action.id">
            <span class="annotation-date">{{ action.exDate }}</span>
            <span class="annotation-label">{{ action.actionType }}</span>
            <span v-if="actionDetail(action)" class="annotation-detail">{{ actionDetail(action) }}</span>
          </li>
        </ul>
      </section>

      <section v-if="props.findings.length" aria-labelledby="findings-heading">
        <h3 id="findings-heading">Open data-quality findings</h3>
        <ul class="annotation-list">
          <li v-for="finding in props.findings" :key="finding.id">
            <span class="annotation-date">{{ finding.sessionDate ?? 'Whole instrument' }}</span>
            <span class="annotation-label">{{ finding.rule }}</span>
            <span class="annotation-status">{{ finding.status }}</span>
            <span v-if="finding.detail" class="annotation-detail">{{ finding.detail }}</span>
          </li>
        </ul>
      </section>

      <section v-if="props.missingSessions.length" aria-labelledby="missing-heading">
        <h3 id="missing-heading">Missing sessions</h3>
        <p class="annotation-note">
          The exchange was open on {{ props.missingSessions.length }}
          {{ props.missingSessions.length === 1 ? 'session' : 'sessions' }} in this window with
          no stored bar. The chart interrupts the series at these dates rather than joining
          across them. Days the exchange was closed are not listed here and are not gaps.
        </p>
        <ul class="annotation-list">
          <li v-for="session in props.missingSessions" :key="session">
            <span class="annotation-date">{{ session }}</span>
            <span class="annotation-label">no stored observation</span>
          </li>
        </ul>
      </section>
    </template>
  </section>
</template>

<style scoped>
.annotations {
  margin-block-start: 1.5rem;
}

.annotation-list {
  list-style: none;
  padding: 0;
  margin: 0 0 1rem;
  display: grid;
  gap: 0.4rem;
}

.annotation-list li {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: baseline;
  padding-block: 0.35rem;
  border-block-end: 1px solid var(--p-content-border-color, rgb(0 0 0 / 0.12));
}

.annotation-date {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}

.annotation-label {
  text-transform: capitalize;
}

.annotation-status,
.annotation-detail {
  opacity: 0.8;
}

.annotation-note {
  margin-block: 0 0.75rem;
}
</style>
