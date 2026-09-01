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
 * annotation also appears here, in words, always visible.
 *
 * Findings are grouped by rule rather than listed one per row. The explanation belongs to the
 * rule, not to each session it affected, so repeating it produced a wall of identical
 * sentences that buried the dates — which are the part worth seeing. Stating it once and
 * listing the sessions beneath still gives rule, sessions and status, which is what FR-016
 * asks for, and makes the scale of a problem legible at a glance instead of by scrolling.
 */

const props = withDefaults(defineProps<{
  actions: CorporateAction[];
  findings: QualityFinding[];
  missingSessions: string[];
}>(), { missingSessions: () => [] });

/** `missing_session` reads as an identifier. Someone reading a report wants a phrase. */
function ruleLabel(rule: string): string {
  const words = rule.replace(/_/g, ' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
}

interface FindingGroup {
  rule: string;
  label: string;
  status: string;
  detail: string | null;
  sessions: string[];
  undated: number;
}

/** One entry per rule and status, carrying the sessions it affected and its shared explanation. */
const findingGroups = computed<FindingGroup[]>(() => {
  const groups = new Map<string, FindingGroup>();
  for (const finding of props.findings) {
    const key = `${finding.rule}|${finding.status}`;
    const group = groups.get(key) ?? {
      rule: finding.rule,
      label: ruleLabel(finding.rule),
      status: finding.status,
      detail: finding.detail,
      sessions: [],
      undated: 0,
    };
    if (finding.sessionDate) group.sessions.push(finding.sessionDate);
    else group.undated += 1;
    groups.set(key, group);
  }
  return [...groups.values()]
    .map((group) => ({ ...group, sessions: [...group.sessions].sort() }))
    .sort((left, right) => right.sessions.length - left.sessions.length);
});

function sessionCount(count: number): string {
  return `${count} ${count === 1 ? 'session' : 'sessions'}`;
}

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

    <p v-if="!hasAnything" class="annotations__clear">
      Nothing in this window needs explaining: no corporate actions, no open data-quality
      findings, and no missing sessions.
    </p>

    <div v-else class="annotations__groups">
      <section v-if="props.actions.length" aria-labelledby="actions-heading">
        <h3 id="actions-heading">
          Corporate actions
          <span class="annotations__count">{{ props.actions.length }}</span>
        </h3>
        <p class="annotations__why">
          Recorded at the session they took effect. An unadjusted one looks exactly like a real
          move in the price.
        </p>
        <ul class="annotations__rows">
          <li v-for="action in props.actions" :key="action.id">
            <time class="annotations__date">{{ action.exDate }}</time>
            <span class="annotations__label">{{ ruleLabel(action.actionType) }}</span>
            <span v-if="actionDetail(action)" class="annotations__detail">{{ actionDetail(action) }}</span>
          </li>
        </ul>
      </section>

      <section v-if="findingGroups.length" aria-labelledby="findings-heading">
        <h3 id="findings-heading">
          Data-quality findings
          <span class="annotations__count">{{ props.findings.length }}</span>
        </h3>
        <ul class="annotations__rows annotations__rows--grouped">
          <li v-for="group in findingGroups" :key="`${group.rule}|${group.status}`">
            <p class="annotations__group-head">
              <span class="annotations__label">{{ group.label }}</span>
              <span class="annotations__count">{{ sessionCount(group.sessions.length + group.undated) }}</span>
              <span class="annotations__status">{{ group.status }}</span>
            </p>
            <p v-if="group.detail" class="annotations__detail">{{ group.detail }}</p>
            <ol v-if="group.sessions.length" class="annotations__dates">
              <li v-for="session in group.sessions" :key="session">
                <time>{{ session }}</time>
              </li>
            </ol>
            <p v-if="group.undated" class="annotations__detail">
              {{ group.undated }} not tied to a session
            </p>
          </li>
        </ul>
      </section>

      <section v-if="props.missingSessions.length" aria-labelledby="missing-heading">
        <h3 id="missing-heading">
          Missing sessions
          <span class="annotations__count">{{ props.missingSessions.length }}</span>
        </h3>
        <p class="annotations__why">
          The exchange was open and no bar is stored. The chart interrupts the series at these
          dates rather than joining across them. A day the exchange was closed is not listed
          here and is not a gap.
        </p>
        <ol class="annotations__dates">
          <li v-for="session in props.missingSessions" :key="session">
            <time>{{ session }}</time>
          </li>
        </ol>
      </section>
    </div>
  </section>
</template>

<style scoped>
.annotations {
  margin-block-start: 2rem;
}

.annotations__clear {
  margin: 0;
  max-width: 46rem;
  opacity: 0.8;
}

/* One column on a phone, side by side once there is room to compare two. */
.annotations__groups {
  display: grid;
  gap: 1.5rem 2.5rem;
  align-items: start;
}

@media (min-width: 900px) {
  .annotations__groups {
    grid-template-columns: repeat(auto-fit, minmax(20rem, 1fr));
  }
}

.annotations h3 {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  margin: 0 0 0.25rem;
  font-size: 1rem;
}

/* The count sits with its heading, so the scale of a problem reads before its detail. */
.annotations__count {
  font-variant-numeric: tabular-nums;
  font-size: 0.85em;
  font-weight: 400;
  opacity: 0.7;
}

.annotations__why {
  margin: 0 0 0.75rem;
  max-width: 40rem;
  font-size: 0.9em;
  opacity: 0.75;
}

.annotations__rows {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: 0.35rem;
}

.annotations__rows > li {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: baseline;
  padding-block: 0.4rem;
  border-block-end: 1px solid var(--p-content-border-color, rgb(0 0 0 / 0.12));
}

.annotations__rows--grouped > li {
  display: block;
  padding-block: 0.6rem;
}

.annotations__group-head {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: baseline;
  margin: 0;
}

.annotations__date,
.annotations__dates time {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}

.annotations__label {
  font-weight: 600;
}

.annotations__status,
.annotations__detail {
  opacity: 0.75;
  font-size: 0.9em;
}

.annotations__detail {
  margin: 0.15rem 0 0;
  max-width: 40rem;
}

/*
 * Dates wrap as a dense row rather than one line each. Eighty of them are then a paragraph a
 * reader can skim, instead of eighty rows to scroll past.
 */
.annotations__dates {
  list-style: none;
  padding: 0;
  margin: 0.4rem 0 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 0.9rem;
  font-size: 0.9em;
}
</style>
