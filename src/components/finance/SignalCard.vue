<script setup lang="ts">
import Message from 'primevue/message';
import Tag from 'primevue/tag';
import type { Signal, SignalAction } from '@/types/marketData';

const props = defineProps<{ signal: Signal | null }>();

/**
 * A signal is a stated view, never an instruction. The severities below colour the action; the
 * word itself is always present, because colour alone would turn a considered view into a
 * traffic light.
 */
function severity(action: SignalAction): 'success' | 'info' | 'warn' | 'danger' | 'secondary' {
  switch (action) {
    case 'BUY': return 'success';
    case 'WATCH': return 'info';
    case 'HOLD': return 'secondary';
    case 'REDUCE': return 'warn';
    default: return 'danger';
  }
}

const absenceWording: Record<string, string> = {
  insufficient_history: 'there is too little stored history to reach the strategy\'s longest window',
  feature_unavailable: 'the engine recorded no usable value for any of the strategy\'s factors',
  composite_undefined: 'the universe composite was undefined for this session',
  liquidity_excluded: 'the strategy\'s liquidity rule excludes this instrument',
};

function absence(reason: string): string {
  return absenceWording[reason] ?? reason.replace(/_/g, ' ');
}

function decimal(value: string | null, places = 2): string {
  if (value === null) return '—';
  return Number(value).toFixed(places);
}

/**
 * Confidence is agreement among the factors, scaled by how much of the strategy's weight was
 * available. It is not the probability that the view is correct, and the wording says so where
 * the number is, because a bare percentage next to a score reads as one.
 */
function agreement(value: string | null): string {
  if (value === null) return '—';
  return `${(Number(value) * 100).toFixed(0)}% factor agreement`;
}
</script>

<template>
  <section class="signal-card" data-testid="signal-card" aria-labelledby="signal-card-heading">
    <h3 id="signal-card-heading" class="signal-card__heading">What a strategy makes of this</h3>

    <Message v-if="props.signal === null" severity="info" :closable="false">
      No strategy has recorded a view of this instrument. Signals are computed after the
      features they read, so a newly listed instrument has none until the next run.
    </Message>

    <template v-else>
      <div v-if="props.signal.action !== null" class="signal-card__view">
        <Tag :severity="severity(props.signal.action)" :value="props.signal.action" />
        <p class="signal-card__score">
          Score {{ decimal(props.signal.score) }} of a possible 1.00,
          {{ agreement(props.signal.confidence) }}
        </p>
      </div>

      <Message v-else severity="secondary" :closable="false" data-testid="signal-absence">
        No view: {{ absence(props.signal.absenceReason ?? '') }}.
      </Message>

      <dl class="signal-card__meta">
        <div>
          <dt>As of</dt>
          <dd>{{ props.signal.sessionDate }}</dd>
        </div>
        <div>
          <dt>Strategy</dt>
          <dd>
            {{ props.signal.strategy.name }} v{{ props.signal.strategy.version }}
            <span v-if="props.signal.strategy.superseded" class="signal-card__superseded">
              (superseded; kept as the version that produced this)
            </span>
          </dd>
        </div>
      </dl>

      <p class="signal-card__caveat">
        This is a strategy output, not advice. {{ props.signal.strategy.caveat }}
      </p>
    </template>
  </section>
</template>

<style scoped>
.signal-card__heading {
  margin: 0 0 0.75rem;
  font-size: 1.05rem;
}

.signal-card__view {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
}

.signal-card__score {
  margin: 0;
}

.signal-card__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 1.5rem;
  margin: 1rem 0 0;
}

.signal-card__meta dt {
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.signal-card__meta dd {
  margin: 0.125rem 0 0;
}

.signal-card__superseded,
.signal-card__caveat {
  color: var(--p-text-muted-color);
}

.signal-card__caveat {
  margin: 1rem 0 0;
  max-width: 68ch;
  font-size: 0.875rem;
}
</style>
