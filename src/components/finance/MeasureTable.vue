<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Message from 'primevue/message';
import type { BacktestBenchmark, BacktestMeasures } from '@/types/marketData';

const props = defineProps<{
  measures: BacktestMeasures;
  benchmarks: BacktestBenchmark[];
  currency: string;
}>();

/**
 * The six figures, side by side with each benchmark's.
 *
 * They are shown together or not at all. A result free to show a subset shows the flattering one,
 * and the two that flatter least — maximum drawdown and what the trading cost — are exactly the
 * two a chart makes easy to leave out. A figure that could not be computed states its reason in
 * the cell rather than being omitted from the table.
 */

interface Row {
  label: string;
  hint: string;
  read: (measures: BacktestMeasures) => string;
}

function percent(value: string | null): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  return `${(parsed * 100).toFixed(2)}%`;
}

function money(value: string | null): string {
  if (value === null) return '—';
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '—';
  return `${parsed.toLocaleString(undefined, { maximumFractionDigits: 0 })} ${props.currency}`;
}

const rows: Row[] = [
  {
    label: 'Total return',
    hint: 'What the whole period did, after every stated cost.',
    read: (m) => percent(m.totalReturn),
  },
  {
    label: 'Annualised return',
    hint: 'The same result expressed per year. A short period annualises to a large number and means no more for it.',
    read: (m) => percent(m.annualisedReturn),
  },
  {
    label: 'Volatility',
    hint: 'How much the value moved session to session, annualised.',
    read: (m) => percent(m.volatility),
  },
  {
    label: 'Maximum drawdown',
    hint: 'The worst fall from a high point — what holding this would actually have put you through.',
    read: (m) => percent(m.maximumDrawdown),
  },
  {
    label: 'Trades',
    hint: 'How many executions the rules produced.',
    read: (m) => (m.tradeCount === null ? '—' : String(m.tradeCount)),
  },
  {
    label: 'Cost of trading',
    hint: 'Brokerage, slippage and currency spread. Reported beside the return, never below it.',
    read: (m) => money(m.totalCosts),
  },
];

function cellFor(benchmark: BacktestBenchmark, row: Row): string {
  if (benchmark.absenceReason) return '—';
  return row.read(benchmark.measures);
}

const unavailableReason: Record<string, string> = {
  series_starts_after_range: 'the index series begins after this backtest does',
  series_ends_before_range: 'the index series ends before this backtest does',
  insufficient_sessions: 'there are too few sessions to measure',
};

function describeAbsence(reason: string): string {
  return unavailableReason[reason] ?? reason;
}

const unavailable = () => props.benchmarks.filter((benchmark) => benchmark.absenceReason !== null);
</script>

<template>
  <section class="measures" aria-labelledby="measures-heading">
    <h3 id="measures-heading">What happened, and what it is being compared with</h3>

    <Message v-if="props.measures.absenceReason" severity="warn" :closable="false">
      No figures could be computed for this result: {{ describeAbsence(props.measures.absenceReason) }}.
    </Message>

    <DataTable
      v-else
      :value="rows"
      data-testid="measure-table"
      class="measures__table"
    >
      <Column header="Measure" :pt="{ bodyCell: { 'data-label': 'Measure' } }">
        <template #body="{ data }">
          <span class="measures__label">{{ data.label }}</span>
          <span class="measures__hint">{{ data.hint }}</span>
        </template>
      </Column>
      <Column header="This strategy" :pt="{ bodyCell: { 'data-label': 'This strategy' } }">
        <template #body="{ data }">
          <span class="measures__value">{{ data.read(props.measures) }}</span>
        </template>
      </Column>
      <Column
        v-for="benchmark in props.benchmarks"
        :key="benchmark.series"
        :header="benchmark.series"
        :pt="{ bodyCell: { 'data-label': benchmark.series } }"
      >
        <template #body="{ data }">
          <span class="measures__value measures__value--benchmark">{{ cellFor(benchmark, data) }}</span>
        </template>
      </Column>
    </DataTable>

    <Message
      v-for="benchmark in unavailable()"
      :key="benchmark.series"
      severity="info"
      :closable="false"
      class="measures__absence"
    >
      {{ benchmark.series }} cannot be compared over this range because
      {{ describeAbsence(benchmark.absenceReason ?? '') }}. It is left out rather than measured
      over a shorter window, which would look like a comparison and would not be one.
    </Message>
  </section>
</template>

<style scoped>
.measures__label {
  display: block;
  font-weight: 600;
}

.measures__hint {
  display: block;
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
  max-width: 44ch;
}

.measures__value {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}

/* A benchmark is context, not a verdict. It reads in the muted weight so the strategy's own
   figure stays the one being judged, and no direction is ever carried by colour alone. */
.measures__value--benchmark {
  font-weight: 500;
  color: var(--p-text-muted-color);
}

.measures__table {
  --p-datatable-row-padding: 0.75rem;
}

.measures__absence {
  margin-top: 0.75rem;
}
</style>
