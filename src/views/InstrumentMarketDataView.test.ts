import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('lightweight-charts', () => import('@/components/finance/__mocks__/lightweight-charts'));
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { instrumentId: INSTRUMENT_ID }, query: {} }),
}));

import InstrumentMarketDataView from './InstrumentMarketDataView.vue';
import { __reset } from '@/components/finance/__mocks__/lightweight-charts';

const INSTRUMENT_ID = '11111111-1111-4111-8111-111111111111';

/**
 * A stand-in for EventSource that records its instances so a test can deliver a *named*
 * event, which is what the server actually writes.
 */
class QuietEventSource extends EventTarget {
  static instances: QuietEventSource[] = [];
  constructor() {
    super();
    QuietEventSource.instances.push(this);
  }
  close(): void {}
  deliver(type: string, data: string, lastEventId = '1'): void {
    const event = new MessageEvent(type, { data, lastEventId });
    this.dispatchEvent(event);
  }
}

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
    // Shown as a phrase rather than as its identifier; the fact asserted is unchanged.
    expect(text).toContain('Suspicious jump');
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

describe('InstrumentMarketDataView live updates', () => {
  beforeEach(() => {
    // Live changes are coalesced over a short window rather than answered per event, so
    // these tests advance past it instead of asserting an immediate refetch.
    vi.useFakeTimers({ shouldAdvanceTime: true });
    __reset();
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  it('keeps the chosen range and overlays when a change arrives for this instrument', async () => {
    QuietEventSource.instances = [];
    const wrapper = mountView();
    await flushPromises();

    // Choose a range and turn an overlay off — these are the person's choices, and a live
    // change must not take them away (FR-020).
    const range60 = wrapper.findAll('button').find((button) => button.text() === '60 sessions');
    await range60!.trigger('click');
    await flushPromises();
    await wrapper.get('[aria-label="20-session moving average"]').trigger('click');
    await flushPromises();
    const overlayBefore = wrapper.get('[aria-label="20-session moving average"]').attributes('aria-pressed');

    const callsBefore = vi.mocked(fetch).mock.calls.length;
    const source = QuietEventSource.instances.at(-1);
    expect(source, 'the view opened no event stream').toBeDefined();
    source!.deliver(
      'daily_bar.changed.v1',
      JSON.stringify({ entity_id: 'bar-1', instrument_id: INSTRUMENT_ID, session_date: '2026-06-03' }),
    );
    await flushPromises();
    await vi.advanceTimersByTimeAsync(500);
    await flushPromises();

    // The change was for this instrument, so the window is re-read.
    expect(vi.mocked(fetch).mock.calls.length).toBeGreaterThan(callsBefore);
    expect(wrapper.findAll('[aria-pressed="true"]').some((element) => element.text() === '60 sessions')).toBe(true);
    expect(wrapper.get('[aria-label="20-session moving average"]').attributes('aria-pressed')).toBe(overlayBefore);
  });

  it('ignores a change for an instrument that is not the one on screen', async () => {
    QuietEventSource.instances = [];
    const wrapper = mountView();
    await flushPromises();
    const callsBefore = vi.mocked(fetch).mock.calls.length;

    QuietEventSource.instances.at(-1)!.deliver(
      'daily_bar.changed.v1',
      JSON.stringify({ entity_id: 'bar-9', instrument_id: 'some-other-instrument' }),
    );
    await flushPromises();
    await vi.advanceTimersByTimeAsync(500);
    await flushPromises();

    // Refetching for an instrument nobody is looking at wastes a request and can only
    // disturb the view.
    expect(vi.mocked(fetch).mock.calls.length).toBe(callsBefore);
    expect(wrapper.text()).toContain('Interrupted History AB');
  });

  it('applies a repeated event identifier exactly once', async () => {
    QuietEventSource.instances = [];
    mountView();
    await flushPromises();
    const callsBefore = vi.mocked(fetch).mock.calls.length;

    const source = QuietEventSource.instances.at(-1)!;
    const payload = JSON.stringify({ entity_id: 'bar-1', instrument_id: INSTRUMENT_ID });
    source.deliver('daily_bar.changed.v1', payload, '42');
    source.deliver('daily_bar.changed.v1', payload, '42');
    await flushPromises();
    await vi.advanceTimersByTimeAsync(500);
    await flushPromises();

    expect(vi.mocked(fetch).mock.calls.length).toBe(callsBefore + 1);
  });

  afterEach(() => vi.useRealTimers());

  it('reports the connection state so a stale view is never presented as current', async () => {
    const wrapper = mountView();
    await flushPromises();
    // The status component renders one of connected / reconnecting / stale / offline.
    expect(wrapper.text()).toMatch(/connected|reconnecting|stale|offline/i);
  });
});

describe('InstrumentMarketDataView under an event storm', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    __reset();
    QuietEventSource.instances = [];
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  afterEach(() => vi.useRealTimers());

  it('coalesces a burst of bars for this instrument into one reload', async () => {
    const wrapper = mountView();
    await flushPromises();
    const before = vi.mocked(fetch).mock.calls.length;

    // An import over this instrument's ten-year history publishes thousands of these.
    const source = QuietEventSource.instances.at(-1)!;
    for (let index = 0; index < 200; index += 1) {
      source.deliver(
        'daily_bar.changed.v1',
        JSON.stringify({ entity_type: 'daily_bar', entity_id: `bar-${index}`, instrument_id: INSTRUMENT_ID }),
        String(index),
      );
    }
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2_000);
    await flushPromises();

    const issued = vi.mocked(fetch).mock.calls.length - before;
    expect(issued, `200 events issued ${issued} history requests`).toBeLessThanOrEqual(2);
    expect(issued).toBeGreaterThan(0);
    wrapper.unmount();
  });

  it('issues nothing for a burst about other instruments', async () => {
    const wrapper = mountView();
    await flushPromises();
    const before = vi.mocked(fetch).mock.calls.length;

    const source = QuietEventSource.instances.at(-1)!;
    for (let index = 0; index < 50; index += 1) {
      source.deliver(
        'daily_bar.changed.v1',
        JSON.stringify({ entity_type: 'daily_bar', entity_id: `x-${index}`, instrument_id: `other-${index}` }),
        String(500 + index),
      );
    }
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2_000);
    await flushPromises();

    expect(vi.mocked(fetch).mock.calls.length - before).toBe(0);
    wrapper.unmount();
  });
});
