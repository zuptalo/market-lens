import { mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { describe, expect, it } from 'vitest';
import InstrumentTable from './InstrumentTable.vue';
import {
  buildListingRow,
  buildRowWithNoHistory,
  buildRowWithTooFewSessions,
} from '@/services/__fixtures__/marketData';

function mountTable(overrides: Record<string, unknown> = {}) {
  return mount(InstrumentTable, {
    global: { plugins: [PrimeVue] },
    props: {
      rows: [buildListingRow(), buildRowWithTooFewSessions(), buildRowWithNoHistory()],
      loading: false,
      sort: 'name',
      order: 'asc',
      visibleColumns: ['sector', 'return20', 'volatility'],
      ...overrides,
    },
  });
}

describe('InstrumentTable', () => {
  it('asks the server to reorder the whole result set instead of sorting the page it holds', async () => {
    const wrapper = mountTable();
    const sortableHeader = wrapper.findAll('th').find((th) => th.text().includes('20-session'));
    expect(sortableHeader).toBeTruthy();
    await sortableHeader!.trigger('click');

    // Sorting the rows already fetched would reorder three rows and quietly mislead: page two
    // holds values that belong on page one. The table's only correct response is to ask for a
    // new ordering (FR-005).
    expect(wrapper.emitted('sort')).toBeTruthy();
    expect(wrapper.emitted('sort')![0][0]).toEqual({ sort: 'return_20', order: 'asc' });
  });

  it('preserves the order the server returned rather than re-sorting locally', () => {
    const rows = [
      buildListingRow({ id: 'b', name: 'Beta AB' }),
      buildListingRow({ id: 'a', name: 'Alpha AB' }),
    ];
    const wrapper = mountTable({ rows });
    const text = wrapper.text();
    // The server ordered these; a table that re-sorts by name would swap them and would then
    // disagree with the cursor that produced them.
    expect(text.indexOf('Beta AB')).toBeLessThan(text.indexOf('Alpha AB'));
  });

  it('shows an uncomputable statistic as an explicit absence, never as zero', () => {
    const wrapper = mountTable({ rows: [buildRowWithTooFewSessions()] });
    const text = wrapper.text();
    expect(text).not.toContain('0.00%');
    // The reader is told why the value is missing rather than being shown a number that
    // looks like an observation.
    expect(text).toMatch(/—|Not enough sessions/);
  });

  it('tells an instrument with no history apart from one that simply has not moved', () => {
    const wrapper = mountTable({ rows: [buildRowWithNoHistory()] });
    const text = wrapper.text();
    expect(text).toContain('No stored history');
    expect(text).not.toContain('0 sessions behind');
  });

  it('states each price in its own currency and never implies a comparison', () => {
    const wrapper = mountTable();
    expect(wrapper.text()).toContain('SEK');
    expect(wrapper.text()).toContain('DKK');
  });

  it('hides an optional column the device has turned off', () => {
    const wrapper = mountTable({ visibleColumns: ['sector'] });
    const headers = wrapper.findAll('th').map((th) => th.text());
    expect(headers.join(' ')).toContain('Sector');
    expect(headers.join(' ')).not.toContain('Volatility');
  });

  it('trims the trailing zeros a numeric(20,8) column brings with it', () => {
    // What real data actually looks like. Every fixture in this suite used two-decimal
    // strings, so the suite happily passed while production rendered
    // "21540.00000000 DKK" — a price that reads as noise.
    const wrapper = mountTable({
      rows: [buildListingRow({
        latestClose: '21540.00000000',
        changeAbsolute: '-430.00000000',
        currency: 'DKK',
      })],
    });
    const text = wrapper.text();
    expect(text).toContain('21540.00 DKK');
    expect(text).toContain('-430.00');
    expect(text).not.toContain('21540.00000000');
  });

  it('keeps precision the provider actually reported', () => {
    const wrapper = mountTable({
      rows: [buildListingRow({ latestClose: '0.12345678', currency: 'SEK' })],
    });
    // Rounding here would state a price the data does not support.
    expect(wrapper.text()).toContain('0.12345678 SEK');
  });

  it('labels every cell so the table can stack into cards on a narrow screen', () => {
    const wrapper = mountTable();
    // The mobile treatment is a stacked card per instrument; each cell carries its own label
    // so it stays readable once the header row is no longer beside it.
    const labelled = wrapper.findAll('td[data-label]');
    expect(labelled.length).toBeGreaterThan(0);
    expect(labelled.map((cell) => cell.attributes('data-label'))).toContain('Close');
  });
});

// Feature 014 US3: "unclassified" is a value the reader sees, not an empty cell that reads as
// broken data — which is what every row showed before the column was populated.
describe('InstrumentTable sector', () => {
  it('states that an instrument is unclassified rather than leaving the cell blank', () => {
    const wrapper = mount(InstrumentTable, {
      global: { plugins: [PrimeVue] },
      props: {
        rows: [buildListingRow({ sector: 'unclassified', sectorName: 'Unclassified' })],
        loading: false,
        sort: 'name',
        order: 'asc',
        visibleColumns: ['sector'],
      },
    });
    expect(wrapper.text()).toContain('Unclassified');
  });

  it('shows the sector name a person reads, not the code the filter uses', () => {
    const wrapper = mount(InstrumentTable, {
      global: { plugins: [PrimeVue] },
      props: {
        rows: [buildListingRow({ sector: 'health_care', sectorName: 'Health Care' })],
        loading: false,
        sort: 'name',
        order: 'asc',
        visibleColumns: ['sector'],
      },
    });
    expect(wrapper.text()).toContain('Health Care');
    expect(wrapper.text()).not.toContain('health_care');
  });
});

describe('InstrumentTable while loading', () => {
  // A table's own overlay covers the table, and before any rows exist the table is only as tall
  // as its header row — so the spinner landed on the column headings, pointing at the one part of
  // the screen that was already finished.
  it('shows a first load where the rows will be, not over the headings', () => {
    const wrapper = mount(InstrumentTable, {
      props: { rows: [], loading: true, sort: 'name', order: 'asc', visibleColumns: [] },
      global: { plugins: [PrimeVue] },
    });
    expect(wrapper.find('[data-testid="loading-block"]').exists()).toBe(true);
    expect(wrapper.find('.p-datatable-mask').exists()).toBe(false);
    expect(wrapper.text().toLowerCase()).toContain('loading');
  });

  // A refresh is different: the reader can still see what they had and where new rows will land,
  // so dimming the existing table in place is right.
  it('keeps the rows visible while refreshing them', () => {
    const wrapper = mount(InstrumentTable, {
      props: { rows: [buildListingRow({ name: 'Interrupted History AB' })], loading: true, sort: 'name', order: 'asc', visibleColumns: [] },
      global: { plugins: [PrimeVue] },
    });
    expect(wrapper.find('[data-testid="loading-block"]').exists()).toBe(false);
    expect(wrapper.text()).toContain('Interrupted History AB');
  });
});
