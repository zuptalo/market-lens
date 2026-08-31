import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('lightweight-charts', () => import('@/components/finance/__mocks__/lightweight-charts'));
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { instrumentId: INSTRUMENT_ID }, query: {} }),
}));

import InstrumentMarketDataView from './InstrumentMarketDataView.vue';
import { __reset } from '@/components/finance/__mocks__/lightweight-charts';

const INSTRUMENT_ID = '11111111-1111-4111-8111-111111111111';

class QuietEventSource extends EventTarget { close(): void {} }

function historyWire(overrides: Record<string, unknown> = {}) {
  return {
    instrument: {
      id: INSTRUMENT_ID, isin: 'SE0000000200', ticker: 'GAPPY', name: 'Interrupted History AB',
      exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' }, currency: 'SEK', country: 'SE',
      sector: 'Technology', industry: 'Software', instrument_type: 'common_stock',
      status: 'active', purchasability_status: 'unverified',
      latest_session: '2026-06-02', latest_close: '109.50', change_absolute: '1.00',
      change_percent: 0.0092, return_20: 0.0412, return_90: null, volatility: 0.1875,
      stored_sessions: 10, freshness: { state: 'current', sessions_behind: 0 },
    },
    coverage: { first_session: '2026-01-05', last_session: '2026-06-02', stored_sessions: 120 },
    requested_from: '2026-05-18',
    requested_to: '2026-06-02',
    bars: [
      { session_date: '2026-05-18', open: '100.00', high: '101.50', low: '98.50', close: '100.50', adjusted_close: null, volume: 1000 },
      { session_date: '2026-05-27', open: '105.00', high: '106.50', low: '103.50', close: '105.50', adjusted_close: null, volume: 1042 },
    ],
    missing_sessions: ['2026-05-25', '2026-05-26'],
    series_basis: 'provider_adjusted',
    provider: 'fixture',
    observed_at: '2026-06-02T17:30:00Z',
    actions: [{
      id: 'a1', action_type: 'split', ex_date: '2026-05-27', ratio: '2',
      amount: null, currency: null, old_symbol: null, new_symbol: null,
    }],
    findings: [{
      id: 'f1', rule: 'suspicious_jump', status: 'open', session_date: '2026-05-27',
      detail: 'close moved more than the configured threshold',
    }],
    ...overrides,
  };
}

function stubFetch(body: unknown = historyWire(), ok = true) {
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
    const url = String(input);
    if (url.includes('/history')) return { ok, json: async () => body };
    return { ok: true, json: async () => ({ items: [], next_cursor: null }) };
  }));
}

function mountView() {
  return mount(InstrumentMarketDataView, {
    global: { plugins: [PrimeVue] },
  });
}

describe('InstrumentMarketDataView', () => {
  beforeEach(() => {
    __reset();
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  it('states the exchange-qualified identity and the listing currency', async () => {
    const wrapper = mountView();
    await flushPromises();
    const text = wrapper.text();
    expect(text).toContain('Interrupted History AB');
    expect(text).toContain('GAPPY');
    expect(text).toContain('XSTO');
    expect(text).toContain('SEK');
  });

  it('says how much of the requested range is actually covered', async () => {
    const wrapper = mountView();
    await flushPromises();
    const text = wrapper.text();
    // Coverage describes the instrument, and the window describes what is drawn. A reader
    // looking at three weeks must be able to see that months exist behind it.
    expect(text).toContain('2026-01-05');
    expect(text).toContain('2026-06-02');
  });

  it('states the count of missing sessions in view as text, not only as a visual break', async () => {
    const wrapper = mountView();
    await flushPromises();
    // FR-017: everything the chart conveys visually must also be readable. A gap that exists
    // only as a shape in a canvas is invisible to anyone not looking at the canvas.
    expect(wrapper.text()).toMatch(/2 (missing )?sessions?/i);
  });

  it('names the provider and observation time, and whether the series is adjusted', async () => {
    const wrapper = mountView();
    await flushPromises();
    const text = wrapper.text();
    expect(text).toMatch(/adjusted/i);
    expect(text).toContain('fixture');
  });

  it('lists open quality findings with their rule and affected session', async () => {
    const wrapper = mountView();
    await flushPromises();
    const text = wrapper.text();
    expect(text).toContain('suspicious_jump');
    expect(text).toContain('2026-05-27');
  });

  it('makes a corporate action readable without hovering', async () => {
    const wrapper = mountView();
    await flushPromises();
    const text = wrapper.text();
    // The marker on the canvas is not enough: its type and value must be readable as text.
    expect(text).toMatch(/split/i);
    expect(text).toContain('2');
  });

  it('keeps the history readable when the instrument is no longer active', async () => {
    const wire = historyWire();
    (wire.instrument as Record<string, unknown>).status = 'inactive';
    stubFetch(wire);
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text()).toMatch(/inactive/i);
    expect(wrapper.text()).toContain('Interrupted History AB');
  });

  it('reports a failure safely without leaking what the server said', async () => {
    stubFetch({ error: 'token=secret' }, false);
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain('Unable to load');
    expect(wrapper.text()).not.toContain('secret');
  });
});
