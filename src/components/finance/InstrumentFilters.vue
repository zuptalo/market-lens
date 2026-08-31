<script setup lang="ts">
/**
 * The universe filters.
 *
 * Extracted into their own component so exactly one instance exists at a time: above the
 * list on tablet and desktop, inside a drawer below the tablet breakpoint. Rendering both
 * and hiding one with CSS would put two controls with the same accessible name into the
 * page, which is worse for a screen reader than the layout problem it solves.
 *
 * The native select and search input are deliberate. PrimeVue's own primitives are preferred
 * across this project, but these five controls are exactly the case where the platform
 * control is better: it is keyboard-operable everywhere, it uses the device's native picker
 * on a phone, and it needs no JavaScript to stay accessible.
 */

const EXCHANGES = ['XSTO', 'XCSE', 'XHEL', 'XOSL'];
const COUNTRIES = ['SE', 'DK', 'FI', 'NO'];
const SECTORS = [
  'Communication Services', 'Consumer Discretionary', 'Consumer Staples', 'Energy',
  'Financials', 'Health Care', 'Industrials', 'Information Technology', 'Materials',
  'Real Estate', 'Technology', 'Utilities',
];

defineProps<{
  query: string;
  mic: string;
  country: string;
  sector: string;
  status: string;
}>();

defineEmits<{
  (event: 'update:query', value: string): void;
  (event: 'update:mic', value: string): void;
  (event: 'update:country', value: string): void;
  (event: 'update:sector', value: string): void;
  (event: 'update:status', value: string): void;
}>();
</script>

<template>
  <div class="instrument-filters" role="group" aria-label="Instrument filters">
    <label>Search
      <input
        :value="query"
        type="search"
        aria-label="Search instruments"
        placeholder="Ticker, company, or ISIN"
        @input="$emit('update:query', ($event.target as HTMLInputElement).value)"
      >
    </label>
    <label>Exchange
      <select
        :value="mic"
        aria-label="Exchange"
        @change="$emit('update:mic', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">All exchanges</option>
        <option v-for="value in EXCHANGES" :key="value" :value="value">{{ value }}</option>
      </select>
    </label>
    <label>Country
      <select
        :value="country"
        aria-label="Country"
        @change="$emit('update:country', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">All countries</option>
        <option v-for="value in COUNTRIES" :key="value" :value="value">{{ value }}</option>
      </select>
    </label>
    <label>Sector
      <select
        :value="sector"
        aria-label="Sector"
        @change="$emit('update:sector', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">All sectors</option>
        <option v-for="value in SECTORS" :key="value" :value="value">{{ value }}</option>
      </select>
    </label>
    <label>Status
      <select
        :value="status"
        aria-label="Status"
        @change="$emit('update:status', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">All statuses</option>
        <option value="active">Active</option>
        <option value="inactive">Inactive</option>
      </select>
    </label>
  </div>
</template>

<style scoped>
.instrument-filters {
  display: grid;
  gap: 0.75rem;
  grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
  margin-block-end: 1rem;
}

.instrument-filters label {
  display: grid;
  gap: 0.25rem;
}

.instrument-filters input,
.instrument-filters select {
  /* Comfortably operable by touch, and large enough that text does not need zooming. */
  min-height: 2.75rem;
  font-size: 1rem;
  max-width: 100%;
}
</style>
