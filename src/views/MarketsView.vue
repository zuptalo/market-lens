<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import Button from 'primevue/button';
import Drawer from 'primevue/drawer';
import MarketDataStatus from '@/components/finance/MarketDataStatus.vue';
import InstrumentFilters from '@/components/finance/InstrumentFilters.vue';
import InstrumentTable from '@/components/finance/InstrumentTable.vue';
import { useCompactViewport } from '@/composables/useCompactViewport';
import {
  MarketDataLive,
  fetchInstrumentListing,
  fetchRecentImports,
  listingQueryString,
  type LiveEvent,
  type LiveEventPayload,
  type LiveEventSource,
} from '@/services/marketData';
import { OPTIONAL_COLUMNS, useColumnPreference } from '@/stores/instrumentColumns';
import type {
  ConnectionState,
  ImportRunSummary,
  InstrumentListingPage,
  ListingQuery,
  ListingSort,
} from '@/types/marketData';

/**
 * The universe list.
 *
 * Filters, sort and page live in the address bar rather than only in component state, so a
 * reload or a back navigation restores the view a person was looking at (FR-006). The
 * component never sorts or filters what it already holds: every change asks the server for a
 * new result set, because sorting the current page would order three rows and quietly claim
 * to have ordered a hundred.
 */

const COLUMN_LABELS: Record<string, string> = {
  sector: 'Sector', country: 'Country', return20: '20-session return',
  return90: '90-session return', volatility: 'Volatility', storedSessions: 'Stored sessions',
};

const initial = new URLSearchParams(window.location.search);
const query = ref(initial.get('q') ?? '');
const mic = ref(initial.get('mic') ?? '');
const country = ref(initial.get('country') ?? '');
const sector = ref(initial.get('sector') ?? '');
const status = ref(initial.get('status') ?? '');
const sort = ref<ListingSort>((initial.get('sort') as ListingSort) ?? 'name');
const order = ref<'asc' | 'desc'>(initial.get('order') === 'desc' ? 'desc' : 'asc');

const listing = ref<InstrumentListingPage>({ items: [], nextCursor: null });
const instrumentLoading = ref(true);
const instrumentError = ref('');
const loadingMore = ref(false);
const filtersOpen = ref(false);
const columnsOpen = ref(false);

const runs = ref<ImportRunSummary[]>([]);
const loading = ref(true);
const error = ref('');
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');

const preference = useColumnPreference();
const compact = useCompactViewport();
let listingController: AbortController | undefined;
let statusController: AbortController | undefined;
let sequence = 0;

const activeFilters = computed(() => {
  const chips: Array<{ key: string; label: string; clear: () => void }> = [];
  if (query.value) chips.push({ key: 'q', label: `Search: ${query.value}`, clear: () => { query.value = ''; } });
  if (mic.value) chips.push({ key: 'mic', label: `Exchange: ${mic.value}`, clear: () => { mic.value = ''; } });
  if (country.value) chips.push({ key: 'country', label: `Country: ${country.value}`, clear: () => { country.value = ''; } });
  if (sector.value) chips.push({ key: 'sector', label: `Sector: ${sector.value}`, clear: () => { sector.value = ''; } });
  if (status.value) chips.push({ key: 'status', label: `Status: ${status.value}`, clear: () => { status.value = ''; } });
  return chips;
});

function currentQuery(cursor?: string): ListingQuery {
  return {
    query: query.value || undefined,
    mic: mic.value || undefined,
    country: country.value || undefined,
    sector: sector.value || undefined,
    status: (status.value || undefined) as ListingQuery['status'],
    sort: sort.value,
    order: order.value,
    cursor,
    limit: 50,
  };
}

/** The address-bar form of the current view, so a reload lands on the same thing. */
const addressQuery = computed(() => listingQueryString(currentQuery()));

async function load(): Promise<void> {
  const mine = ++sequence;
  listingController?.abort();
  const controller = new AbortController();
  listingController = controller;
  instrumentLoading.value = true;
  instrumentError.value = '';
  window.history.replaceState(
    window.history.state, '',
    `/markets${addressQuery.value ? `?${addressQuery.value}` : ''}`,
  );
  try {
    const page = await fetchInstrumentListing(currentQuery(), fetch, controller.signal);
    if (mine !== sequence) return;
    listing.value = page;
    instrumentLoading.value = false;
  } catch (cause) {
    if (controller.signal.aborted || mine !== sequence) return;
    instrumentLoading.value = false;
    // Never surface what the server said: it can name internal state.
    instrumentError.value = 'Unable to load instruments. Your filters have been preserved.';
    void cause;
  }
}

async function loadMore(): Promise<void> {
  if (!listing.value.nextCursor || loadingMore.value) return;
  loadingMore.value = true;
  try {
    const page = await fetchInstrumentListing(currentQuery(listing.value.nextCursor), fetch);
    listing.value = {
      items: [...listing.value.items, ...page.items],
      nextCursor: page.nextCursor,
    };
  } catch {
    instrumentError.value = 'Unable to load more instruments.';
  } finally {
    loadingMore.value = false;
  }
}

function onSort(next: { sort: ListingSort; order: 'asc' | 'desc' }): void {
  sort.value = next.sort;
  order.value = next.order;
}

function clearFilters(): void {
  query.value = '';
  mic.value = '';
  country.value = '';
  sector.value = '';
  status.value = '';
}

function toggleColumn(column: string): void {
  const next = preference.columns.value.includes(column)
    ? preference.columns.value.filter((entry) => entry !== column)
    : [...preference.columns.value, column];
  preference.setColumns(next);
}

function open(id: string): void {
  window.location.assign(`/markets/${id}?return=${encodeURIComponent(addressQuery.value)}`);
}

watch([query, mic, country, sector, status, sort, order], () => { void load(); });

async function refresh(): Promise<void> {
  statusController?.abort();
  statusController = new AbortController();
  try {
    runs.value = await fetchRecentImports(fetch, statusController.signal);
    error.value = '';
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') return;
    error.value = 'Unable to load market-data status.';
  } finally {
    loading.value = false;
  }
}

function browserEventSource(url: string, lastEventId: string): LiveEventSource {
  const endpoint = lastEventId ? `${url}?last_event_id=${encodeURIComponent(lastEventId)}` : url;
  const source = new EventSource(endpoint);
  return {
    addEventListener(type, listener) {
      source.addEventListener(type, (event) => {
        const message = event as MessageEvent<string>;
        listener({ lastEventId: message.lastEventId ?? '', type, data: message.data ?? '' } satisfies LiveEvent);
      });
    },
    close: () => source.close(),
  };
}

/**
 * Apply a committed change in place.
 *
 * The payload names the instrument the change concerns, so a bar for something that is not
 * on screen changes nothing: the view must not jump, reorder, or scroll to it (FR-020). Only
 * the affected row is refetched, which is what keeps the filters, sort, page and scroll
 * position a person was using.
 */
async function applyLiveChange(entityType: string, payload: LiveEventPayload): Promise<void> {
  if (entityType === 'import_run' || entityType === 'import_item') {
    void refresh();
    return;
  }
  const instrumentId = payload.instrument_id;
  if (!instrumentId) return;
  if (!listing.value.items.some((row) => row.id === instrumentId)) return;

  // Re-read the rows already on screen under the same query, and swap in only the one the
  // event named. Filters, sort, page and scroll position are held outside this ref, so they
  // are untouched; and an instrument outside the current filters never gets here at all, so
  // the view does not jump or reorder for something the person is not looking at (FR-020).
  try {
    const page = await fetchInstrumentListing(
      { ...currentQuery(), limit: listing.value.items.length }, fetch,
    );
    const updated = page.items.find((row) => row.id === instrumentId);
    if (!updated) return;
    listing.value = {
      ...listing.value,
      items: listing.value.items.map((row) => (row.id === instrumentId ? updated : row)),
    };
  } catch {
    // A failed refresh leaves the row as it was rather than blanking it; the connection
    // state already tells the person the view may not be current.
  }
}

const live = new MarketDataLive({
  sourceFactory: browserEventSource,
  onRefresh: (entityType, _entityId, payload) => {
    if (entityType === 'quality_finding') void refresh();
    void applyLiveChange(entityType, payload);
  },
  onState: (state) => { connectionState.value = state; },
  reconnectDelayMs: 1_000,
  staleAfterMs: 10_000,
});
const online = () => live.setOnline(true);
const offline = () => live.setOnline(false);

onMounted(() => {
  void load();
  void refresh();
  live.setOnline(navigator.onLine);
  live.start();
  window.addEventListener('online', online);
  window.addEventListener('offline', offline);
});

onBeforeUnmount(() => {
  listingController?.abort();
  statusController?.abort();
  live.stop();
  window.removeEventListener('online', online);
  window.removeEventListener('offline', offline);
});
</script>

<template>
  <div class="markets-view">
    <header class="page-intro">
      <p class="eyebrow">Data foundation</p>
      <h1>Market data</h1>
      <p>Browse the curated Nordic universe. Every price is stated in its own listing currency; nothing is converted or compared across currencies.</p>
    </header>

    <section class="instrument-browser" aria-labelledby="instrument-browser-heading">
      <div class="status-heading">
        <div>
          <p class="eyebrow">Curated universe</p>
          <h2 id="instrument-browser-heading">Instruments</h2>
        </div>
        <div class="browser-actions">
          <Button
            class="filters-trigger"
            severity="secondary"
            label="Filters"
            data-testid="open-filters"
            @click="filtersOpen = true"
          />
          <Button
            severity="secondary"
            label="Columns"
            data-testid="open-columns"
            @click="columnsOpen = true"
          />
        </div>
      </div>

      <!--
        Exactly one instance of the filters is rendered: above the list when there is room,
        inside the drawer when there is not. Never both, so the page never holds two controls
        with the same accessible name.
      -->
      <InstrumentFilters
        v-if="!compact"
        v-model:query="query"
        v-model:mic="mic"
        v-model:country="country"
        v-model:sector="sector"
        v-model:status="status"
      />

      <ul v-if="activeFilters.length" class="active-filters" aria-label="Active filters">
        <li v-for="chip in activeFilters" :key="chip.key">
          <button type="button" @click="chip.clear()">
            {{ chip.label }}<span aria-hidden="true"> ×</span>
            <span class="sr-only">Remove filter</span>
          </button>
        </li>
      </ul>

      <p v-if="instrumentError" role="alert" class="status-error">{{ instrumentError }}</p>

      <InstrumentTable
        :rows="listing.items"
        :loading="instrumentLoading"
        :sort="sort"
        :order="order"
        :visible-columns="preference.columns.value"
        @sort="onSort"
        @open="open"
      />

      <p
        v-if="!instrumentLoading && !instrumentError && listing.items.length === 0"
        class="empty-state"
        role="status"
      >
        No instruments match these filters.
        <button type="button" data-testid="clear-filters" @click="clearFilters()">
          Clear all filters
        </button>
      </p>

      <Button
        v-if="listing.nextCursor"
        :loading="loadingMore"
        severity="secondary"
        label="Load more"
        data-testid="load-more"
        @click="loadMore()"
      />
    </section>

    <Drawer v-model:visible="filtersOpen" header="Filters" position="bottom" class="filters-drawer">
      <InstrumentFilters
        v-if="compact"
        v-model:query="query"
        v-model:mic="mic"
        v-model:country="country"
        v-model:sector="sector"
        v-model:status="status"
      />
      <p>Filters apply to the whole universe, not only to the rows already loaded.</p>
      <Button label="Clear all filters" severity="secondary" @click="clearFilters(); filtersOpen = false" />
    </Drawer>

    <Drawer v-model:visible="columnsOpen" header="Columns" position="bottom">
      <ul class="column-options">
        <li v-for="column in OPTIONAL_COLUMNS" :key="column">
          <label>
            <input
              type="checkbox"
              :checked="preference.columns.value.includes(column)"
              :aria-label="COLUMN_LABELS[column]"
              @change="toggleColumn(column)"
            >
            {{ COLUMN_LABELS[column] }}
          </label>
        </li>
      </ul>
      <p class="column-note">Remembered on this device.</p>
    </Drawer>

    <MarketDataStatus :runs="runs" :connection-state="connectionState" :loading="loading" :error="error" />
  </div>
</template>

<style scoped>
.browser-actions {
  display: flex;
  gap: 0.5rem;
}

.active-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  list-style: none;
  padding: 0;
  margin: 0 0 1rem;
}

.active-filters button {
  border: 1px solid var(--p-content-border-color, rgb(0 0 0 / 0.2));
  border-radius: 999px;
  padding: 0.25rem 0.75rem;
  /* Comfortably operable by touch without hunting for the target. */
  min-height: 2.75rem;
  background: transparent;
  color: inherit;
  cursor: pointer;
}

.empty-state {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  padding: 1.5rem 0;
}

.column-options {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: 0.5rem;
}

.column-options label {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-height: 2.75rem;
}

.column-note {
  opacity: 0.75;
  font-size: 0.9em;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}

/*
 * Desktop and tablet keep the filters above the list. Below the tablet breakpoint they move
 * into the drawer so the list itself gets the screen.
 */
.filters-trigger {
  display: none;
}

@media (max-width: 767px) {
  .filters-trigger {
    display: inline-flex;
  }

}
</style>
