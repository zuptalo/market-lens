<script setup lang="ts">
import InputText from 'primevue/inputtext';
import Select from 'primevue/select';

/**
 * The universe filters.
 *
 * Extracted into their own component so exactly one instance exists at a time: above the
 * list on tablet and desktop, inside a drawer below the tablet breakpoint. Rendering both
 * and hiding one with CSS would put two controls with the same accessible name into the
 * page, which is worse for a screen reader than the layout problem it solves.
 *
 * Sorting is offered here as well as on the table headers. Below the tablet breakpoint the
 * table stacks into cards and its header row is off screen, so the headers alone would leave
 * a phone with no way to sort at all.
 */

const EXCHANGES = ['XSTO', 'XCSE', 'XHEL', 'XOSL'];
const COUNTRIES = ['SE', 'DK', 'FI', 'NO'];
const SECTORS = [
  'Communication Services', 'Consumer Discretionary', 'Consumer Staples', 'Energy',
  'Financials', 'Health Care', 'Industrials', 'Information Technology', 'Materials',
  'Real Estate', 'Technology', 'Utilities',
];

function options(values: string[], allLabel: string) {
  return [{ label: allLabel, value: '' }, ...values.map((value) => ({ label: value, value }))];
}

const exchangeOptions = options(EXCHANGES, 'All exchanges');
const countryOptions = options(COUNTRIES, 'All countries');
const sectorOptions = options(SECTORS, 'All sectors');
const statusOptions = [
  { label: 'All statuses', value: '' },
  { label: 'Active', value: 'active' },
  { label: 'Inactive', value: 'inactive' },
];

/** Ranges and statistics are named the way the contract names them. */
const sortOptions = [
  { label: 'Name', value: 'name' },
  { label: 'Ticker', value: 'ticker' },
  { label: 'Exchange', value: 'exchange' },
  { label: 'Sector', value: 'sector' },
  { label: 'Country', value: 'country' },
  { label: 'Latest close', value: 'latest_close' },
  { label: 'Change', value: 'change_percent' },
  { label: '20-session return', value: 'return_20' },
  { label: '90-session return', value: 'return_90' },
  { label: 'Volatility', value: 'volatility' },
  { label: 'Freshness', value: 'freshness' },
];

const orderOptions = [
  { label: 'Ascending', value: 'asc' },
  { label: 'Descending', value: 'desc' },
];

defineProps<{
  query: string;
  mic: string;
  country: string;
  sector: string;
  status: string;
  sort: string;
  order: string;
}>();

defineEmits<{
  (event: 'update:query', value: string): void;
  (event: 'update:mic', value: string): void;
  (event: 'update:country', value: string): void;
  (event: 'update:sector', value: string): void;
  (event: 'update:status', value: string): void;
  (event: 'update:sort', value: string): void;
  (event: 'update:order', value: string): void;
}>();
</script>

<template>
  <div class="instrument-filters" role="group" aria-label="Instrument filters">
    <div class="instrument-filters__field">
      <label for="markets-search">Search</label>
      <InputText
        id="markets-search"
        :model-value="query"
        type="search"
        aria-label="Search instruments"
        placeholder="Ticker, company, or ISIN"
        fluid
        @update:model-value="$emit('update:query', $event ?? '')"
      />
    </div>
    <div class="instrument-filters__field">
      <label for="markets-exchange">Exchange</label>
      <Select
        input-id="markets-exchange"
        :model-value="mic"
        :options="exchangeOptions"
        option-label="label"
        option-value="value"
        aria-label="Exchange"
        fluid
        @update:model-value="$emit('update:mic', $event ?? '')"
      />
    </div>
    <div class="instrument-filters__field">
      <label for="markets-country">Country</label>
      <Select
        input-id="markets-country"
        :model-value="country"
        :options="countryOptions"
        option-label="label"
        option-value="value"
        aria-label="Country"
        fluid
        @update:model-value="$emit('update:country', $event ?? '')"
      />
    </div>
    <div class="instrument-filters__field">
      <label for="markets-sector">Sector</label>
      <Select
        input-id="markets-sector"
        :model-value="sector"
        :options="sectorOptions"
        option-label="label"
        option-value="value"
        aria-label="Sector"
        fluid
        @update:model-value="$emit('update:sector', $event ?? '')"
      />
    </div>
    <div class="instrument-filters__field">
      <label for="markets-status">Status</label>
      <Select
        input-id="markets-status"
        :model-value="status"
        :options="statusOptions"
        option-label="label"
        option-value="value"
        aria-label="Status"
        fluid
        @update:model-value="$emit('update:status', $event ?? '')"
      />
    </div>
    <div class="instrument-filters__field">
      <label for="markets-sort">Sort by</label>
      <Select
        input-id="markets-sort"
        :model-value="sort"
        :options="sortOptions"
        option-label="label"
        option-value="value"
        aria-label="Sort by"
        fluid
        @update:model-value="$emit('update:sort', $event ?? 'name')"
      />
    </div>
    <div class="instrument-filters__field">
      <label for="markets-order">Direction</label>
      <Select
        input-id="markets-order"
        :model-value="order"
        :options="orderOptions"
        option-label="label"
        option-value="value"
        aria-label="Sort direction"
        fluid
        @update:model-value="$emit('update:order', $event ?? 'asc')"
      />
    </div>
  </div>
</template>

<style scoped>
/* Layout only. Colour, border, radius and type belong to the theme. */
.instrument-filters {
  display: grid;
  gap: 0.75rem;
  grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
  margin-block-end: 1rem;
}

.instrument-filters__field {
  display: grid;
  gap: 0.25rem;
  min-width: 0;
}
</style>
