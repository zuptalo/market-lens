import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import MarketsView from './MarketsView.vue';

class QuietEventSource extends EventTarget { close(): void {} }

function wireRow(overrides: Record<string, unknown> = {}) {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    isin: 'SE0000000200',
    ticker: 'GAPPY',
    name: 'Interrupted History AB',
    exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' },
    currency: 'SEK',
    country: 'SE',
    sector: 'Technology',
    industry: 'Software',
    instrument_type: 'common_stock',
    status: 'active',
    purchasability_status: 'unverified',
    latest_session: '2026-06-30',
    latest_close: '109.50',
    change_absolute: '1.00',
    change_percent: 0.0092,
    return_20: 0.0412,
    return_90: null,
    volatility: 0.1875,
    stored_sessions: 42,
    freshness: { state: 'current', sessions_behind: 0 },
    ...overrides,
  };
}

let requestedUrls: string[] = [];

function stubFetch(items: unknown[] = [wireRow()]) {
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
    const url = String(input);
    requestedUrls.push(url);
    if (url.includes('/api/v1/instruments?')) {
      return { ok: true, json: async () => ({ items, next_cursor: null }) };
    }
    if (url.includes('/api/v1/market-data/imports')) {
      return { ok: true, json: async () => ({ items: [] }) };
    }
    return { ok: false, json: async () => ({ error: 'not found' }) };
  }));
}

function mountView() {
  return mount(MarketsView, { global: { plugins: [PrimeVue] } });
}

describe('MarketsView', () => {
  beforeEach(() => {
    requestedUrls = [];
    window.localStorage.clear();
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  it('lists each instrument with identity, price in its own currency, and freshness', async () => {
    const wrapper = mountView();
    await flushPromises();
    const text = wrapper.text();
    expect(text).toContain('Interrupted History AB');
    expect(text).toContain('GAPPY');
    expect(text).toContain('XSTO');
    expect(text).toContain('109.50');
    expect(text).toContain('SEK');
    expect(text).toContain('Current');
  });

  it('renders an uncomputed statistic as an absence rather than as a zero move', async () => {
    const wrapper = mountView();
    await flushPromises();
    // return_90 is null in the fixture row. A table showing 0.00% there would read as "this
    // instrument was flat over ninety sessions", which is not what the data says.
    expect(wrapper.text()).not.toContain('0.00%');
  });

  it('offers the sector filter the contract defines', async () => {
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.find('[aria-label="Sector"]').exists()).toBe(true);
  });

  it('asks the server for a new ordering when a column is sorted', async () => {
    const wrapper = mountView();
    await flushPromises();
    requestedUrls = [];
    const header = wrapper.findAll('th').find((th) => th.text().includes('20-session'));
    await header!.trigger('click');
    await flushPromises();
    const listingCall = requestedUrls.find((url) => url.includes('/api/v1/instruments?'));
    expect(listingCall).toBeDefined();
    expect(listingCall).toContain('sort=return_20');
  });

  it('explains an empty result and offers a way back instead of showing a blank table', async () => {
    stubFetch([]);
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text()).toContain('No instruments match these filters');
    expect(wrapper.find('[data-testid="clear-filters"]').exists()).toBe(true);
  });

  it('keeps the filters visible and intact when the listing request fails', async () => {
    const wrapper = mountView();
    await flushPromises();
    const search = wrapper.get('input[aria-label="Search instruments"]');
    vi.mocked(fetch).mockImplementation(async (input) => {
      if (String(input).includes('/api/v1/instruments?')) {
        return { ok: false, json: async () => ({ error: 'token=secret' }) } as Response;
      }
      return { ok: true, json: async () => ({ items: [] }) } as Response;
    });
    await search.setValue('ALFA');
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain('Unable to load instruments');
    // The message must not leak whatever the server said.
    expect(wrapper.text()).not.toContain('secret');
    expect((search.element as HTMLInputElement).value).toBe('ALFA');
  });

  it('mirrors the query into the address bar so a reload keeps the same view', async () => {
    const wrapper = mountView();
    await flushPromises();
    await wrapper.get('input[aria-label="Search instruments"]').setValue('gappy');
    await flushPromises();
    expect(window.location.search).toContain('q=gappy');
  });

  it('sends the contract vocabulary on the wire, not the words the old search used', async () => {
    mountView();
    await flushPromises();
    const listingCall = requestedUrls.find((url) => url.includes('/api/v1/instruments?'));
    expect(listingCall).toBeDefined();
    expect(listingCall).not.toContain('exchange=');
    expect(listingCall).not.toContain('active=');
  });
});
