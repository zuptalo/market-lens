<script setup lang="ts">
import Column from 'primevue/column';
import DataTable, { type DataTableSortEvent } from 'primevue/datatable';
import type { InstrumentListingRow, ListingSort } from '@/types/marketData';

/**
 * The universe list.
 *
 * Two rules shape this component more than anything else:
 *
 *  - It never sorts. Rows arrive in the order the database produced over the *whole* result
 *    set, and reordering them here would silently disagree with the cursor that produced
 *    them: page two holds values that belong on page one (FR-005).
 *  - An absent statistic renders as an absence. Showing 0 would turn "there were too few
 *    stored sessions to compute this" into "this instrument did not move" (FR-007).
 *
 * Every cell carries a data-label so the table can stack into one card per instrument below
 * the tablet breakpoint without a second DOM tree or a JavaScript breakpoint.
 */

const props = defineProps<{
  rows: InstrumentListingRow[];
  loading: boolean;
  sort: ListingSort;
  order: 'asc' | 'desc';
  visibleColumns: string[];
}>();

const emit = defineEmits<{
  (event: 'sort', value: { sort: ListingSort; order: 'asc' | 'desc' }): void;
  (event: 'open', id: string): void;
}>();

/** Absence marker. Paired with an accessible explanation wherever it appears. */
const ABSENT = '—';

function onSort(event: DataTableSortEvent): void {
  const field = typeof event.sortField === 'string' ? event.sortField : props.sort;
  emit('sort', {
    sort: field as ListingSort,
    order: event.sortOrder === -1 ? 'desc' : 'asc',
  });
}

function shows(column: string): boolean {
  return props.visibleColumns.includes(column);
}

function percent(value: number | null): string {
  if (value === null) return ABSENT;
  return `${(value * 100).toFixed(2)}%`;
}

/** Why a statistic is absent, so the marker is never left to be guessed at. */
function absenceReason(row: InstrumentListingRow): string {
  if (row.storedSessions === 0) return 'No stored sessions';
  return `Not enough sessions (${row.storedSessions} stored)`;
}

function statisticTitle(row: InstrumentListingRow, value: number | null): string | undefined {
  return value === null ? absenceReason(row) : undefined;
}

function money(row: InstrumentListingRow): string {
  if (row.latestClose === null) return ABSENT;
  return `${row.latestClose} ${row.currency}`;
}

function changeText(row: InstrumentListingRow): string {
  if (row.changeAbsolute === null || row.changePercent === null) return ABSENT;
  return `${row.changeAbsolute} (${percent(row.changePercent)})`;
}

function freshnessLabel(row: InstrumentListingRow): string {
  switch (row.freshness.state) {
    case 'no_history':
      return 'No stored history';
    case 'stale':
      return `${row.freshness.sessionsBehind ?? 0} sessions behind`;
    default:
      return 'Current';
  }
}
</script>

<template>
  <DataTable
    :value="props.rows"
    :loading="props.loading"
    :sort-field="props.sort"
    :sort-order="props.order === 'desc' ? -1 : 1"
    :lazy="true"
    data-key="id"
    sort-mode="single"
    class="instrument-table"
    aria-label="Curated instrument universe"
    @sort="onSort"
  >
    <Column field="name" header="Instrument" :sortable="true" :pt="{ bodyCell: { 'data-label': 'Instrument' } }">
      <template #body="{ data }">
          <a
            class="instrument-link"
            :href="`/markets/${data.id}`"
            :aria-label="`${data.name}, ${data.ticker}, listed on ${data.exchange.mic}`"
            @click.prevent="emit('open', data.id)"
          >
            <strong>{{ data.name }}</strong>
            <span class="instrument-meta">{{ data.ticker }} · {{ data.exchange.mic }}</span>
          </a>
      </template>
    </Column>

    <Column field="latest_close" header="Close" :sortable="true" :pt="{ bodyCell: { 'data-label': 'Close' } }">
      <template #body="{ data }">{{ money(data) }}</template>
    </Column>

    <Column field="change_percent" header="Change" :sortable="true" :pt="{ bodyCell: { 'data-label': 'Change' } }">
      <template #body="{ data }">{{ changeText(data) }}</template>
    </Column>

    <Column v-if="shows('sector')" field="sector" header="Sector" :sortable="true" :pt="{ bodyCell: { 'data-label': 'Sector' } }">
      <template #body="{ data }">{{ data.sector ?? ABSENT }}</template>
    </Column>

    <Column v-if="shows('country')" field="country" header="Country" :sortable="true" :pt="{ bodyCell: { 'data-label': 'Country' } }">
      <template #body="{ data }">{{ data.country }}</template>
    </Column>

    <Column v-if="shows('return20')" field="return_20" header="20-session" :sortable="true" :pt="{ bodyCell: { 'data-label': '20-session' } }">
      <template #body="{ data }">
        <span :title="statisticTitle(data, data.return20)">{{ percent(data.return20) }}</span>
      </template>
    </Column>

    <Column v-if="shows('return90')" field="return_90" header="90-session" :sortable="true" :pt="{ bodyCell: { 'data-label': '90-session' } }">
      <template #body="{ data }">
        <span :title="statisticTitle(data, data.return90)">{{ percent(data.return90) }}</span>
      </template>
    </Column>

    <Column v-if="shows('volatility')" field="volatility" header="Volatility" :sortable="true" :pt="{ bodyCell: { 'data-label': 'Volatility' } }">
      <template #body="{ data }">
        <span :title="statisticTitle(data, data.volatility)">{{ percent(data.volatility) }}</span>
      </template>
    </Column>

    <Column v-if="shows('storedSessions')" field="stored_sessions" header="Sessions" :pt="{ bodyCell: { 'data-label': 'Sessions' } }">
      <template #body="{ data }">{{ data.storedSessions }}</template>
    </Column>

    <Column field="freshness" header="Freshness" :sortable="true" :pt="{ bodyCell: { 'data-label': 'Freshness' } }">
      <template #body="{ data }">{{ freshnessLabel(data) }}</template>
    </Column>

    <!--
      No empty slot here on purpose. The view owns the empty state, because the message is
      only half of it: the other half is the control that clears the filters, and two status
      messages saying the same thing is worse than one that also offers a way back.
    -->
  </DataTable>
</template>

<style scoped>
.instrument-link {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  text-decoration: none;
  color: inherit;
}

.instrument-link:focus-visible {
  outline: 2px solid var(--p-primary-color, currentColor);
  outline-offset: 2px;
}

.instrument-meta {
  font-size: 0.85em;
  opacity: 0.8;
}

/*
 * Below the tablet breakpoint the table stacks into one card per instrument. Each cell keeps
 * its own label, taken from the header it belongs to, so nothing becomes an unlabelled number
 * once the header row is gone. This is CSS rather than a JavaScript breakpoint so there is
 * one DOM, one accessibility tree, and no reflow flicker.
 */
@media (max-width: 767px) {
  /*
   * Stop being a table.
   *
   * Making only the rows `display: block` is not enough and fails in a way that looks like a
   * styling nitpick and is actually unusable: the table keeps its own layout and its columns'
   * intrinsic width — 880px against a 360px screen — so the container scrolls sideways and
   * every "card" collapses to a fraction of the width, hugging the left edge with the rest of
   * the screen empty. The table, its body and its rows all have to leave table layout
   * together, and the min-width PrimeVue sets for horizontal scrolling has to go with them.
   */
  .instrument-table :deep(.p-datatable-table),
  .instrument-table :deep(.p-datatable-tbody) {
    display: block;
    width: 100%;
    min-width: 0;
  }

  .instrument-table :deep(.p-datatable-table-container) {
    overflow-x: visible;
  }

  /*
   * Two traps here, both of which left a blank 43-pixel band above the first card.
   *
   * The selector must use PrimeVue's own class: a bare `thead` type selector loses to
   * `.p-datatable-thead` and silently does nothing.
   *
   * And the usual visually-hidden recipe — absolute, one pixel, clipped — does not work on a
   * table header. Chrome does not blockify internal table boxes, so position, width and
   * height are all ignored on a `table-header-group` and it keeps its natural height. Only
   * removing it from layout actually removes it.
   *
   * Removing it costs nothing here: once stacked, each cell renders its own label from its
   * data-label attribute, so no cell depends on the header row for meaning, and sorting moves
   * into the filter sheet where a small screen can reach it.
   */
  .instrument-table :deep(.p-datatable-thead) {
    display: none;
  }

  .instrument-table :deep(.p-datatable-tbody > tr) {
    display: block;
    width: 100%;
    margin-block-end: 0.75rem;
    border: 1px solid var(--p-content-border-color, rgb(0 0 0 / 0.15));
    border-radius: 0.5rem;
    padding: 0.5rem 0.75rem;
  }

  .instrument-table :deep(.p-datatable-tbody > tr > td) {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 1rem;
    width: auto;
    border: 0;
    padding: 0.3rem 0;
    text-align: end;
  }

  .instrument-table :deep(.p-datatable-tbody > tr > td::before) {
    content: attr(data-label);
    font-weight: 600;
    text-align: start;
    opacity: 0.75;
    /* The label keeps its own line; the value takes the rest and wraps within it. */
    flex: 0 0 auto;
    white-space: nowrap;
  }

  /* The value side gets the remaining width instead of being squeezed into a column. */
  .instrument-table :deep(.p-datatable-tbody > tr > td > *) {
    min-width: 0;
  }

  /* The identity cell leads the card and needs no repeated label. */
  .instrument-table :deep(.p-datatable-tbody > tr > td:first-child) {
    display: block;
    text-align: start;
    padding-block-end: 0.5rem;
  }

  .instrument-table :deep(.p-datatable-tbody > tr > td:first-child::before) {
    content: none;
  }
}

</style>
