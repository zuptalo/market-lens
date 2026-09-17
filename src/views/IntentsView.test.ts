import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import IntentsView from './IntentsView.vue';

class QuietEventSource extends EventTarget {
  close(): void {}
}

function wireIntent(overrides: Record<string, unknown> = {}) {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    instrument_id: '33333333-3333-4333-8333-333333333333',
    ticker: 'VOLV-B',
    name: 'Volvo B',
    currency: 'SEK',
    direction: 'buy',
    quantity: '120.000000000000',
    price: '281.400000000000',
    costs: '0.000000000000',
    status: 'considering',
    recorded_at: '2026-09-18T09:00:00Z',
    settled_at: null,
    consequence: {
      resulting_quantity: '520.000000000000',
      resulting_value: '146328.000000000000',
      resulting_share: '0.412300000000',
      denominator: '354900.000000000000',
      absence_reason: null,
      limits: [],
    },
    ...overrides,
  };
}

interface Stubs {
  intents?: unknown[];
  refusal?: { code: string; message: string };
  failing?: string[];
}

let calls: { url: string; method: string; body: string }[] = [];

function stubFetch(options: Stubs = {}) {
  calls = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL, init?: RequestInit) => {
    const url = String(input);
    calls.push({ url, method: init?.method ?? 'GET', body: String(init?.body ?? '') });
    if ((options.failing ?? []).some((fragment) => url.includes(fragment))) {
      return { ok: false, status: 500, json: async () => ({ error: 'unavailable' }) };
    }
    if (url.includes('/api/v1/order-intents')) {
      if ((init?.method ?? 'GET') !== 'GET' && options.refusal) {
        return { ok: false, status: 400, json: async () => ({ error: options.refusal }) };
      }
      return {
        ok: true,
        json: async () => ({
          intents: options.intents ?? [wireIntent()],
          evaluated_independently: true,
          records_what_you_are_considering: true,
        }),
      };
    }
    if (url.includes('/api/v1/instruments')) {
      return { ok: true, json: async () => ({ items: [{
        id: '33333333-3333-4333-8333-333333333333', isin: 'SE0000115446', ticker: 'VOLV-B',
        name: 'Volvo B', exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' }, currency: 'SEK',
        country: 'SE', instrument_type: 'common_stock', active: true,
        purchasability_status: 'user_confirmed',
      }], next_cursor: null, total: 1 }) };
    }
    if (url.includes('/api/v1/portfolio')) {
      return { ok: true, json: async () => ({
        accounting_currency: 'SEK', holdings: [], realised: [],
        total: { value: '0', cost: '0', unrealised: '0', realised: '0',
          complete: true, incomplete_reason: null, return_absence: 'cash_is_not_tracked' },
        records_what_you_entered: true,
      }) };
    }
    return { ok: false, json: async () => ({ error: 'not found' }) };
  }));
}

const global = { plugins: [PrimeVue] };

describe('IntentsView', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  // The statement the whole screen is built around, and the reason the feature is safe to ship.
  it('says the product records what you are considering and advises nothing', async () => {
    const wrapper = mount(IntentsView, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('no advice');
    expect(text).toContain('nothing is sent anywhere');
    for (const forbidden of ['we recommend', 'you should', 'suggested', 'place an order']) {
      expect(text).not.toContain(forbidden);
    }
  });

  it('shows what each intent would do', async () => {
    const wrapper = mount(IntentsView, { global });
    await flushPromises();
    expect(wrapper.text()).toContain('VOLV-B');
    expect(wrapper.text()).toContain('520');
    expect(wrapper.text()).toContain('41.2%');
  });

  it('reads nothing about anybody else — no path carries a user', async () => {
    mount(IntentsView, { global });
    await flushPromises();
    const read = calls.find((call) => call.url.includes('/api/v1/order-intents'));
    expect(read?.url).toBe('/api/v1/order-intents');
  });

  it('reports a failure rather than showing an empty screen as if nothing were considered', async () => {
    stubFetch({ failing: ['/api/v1/order-intents'] });
    const wrapper = mount(IntentsView, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('unable to load');
    expect(text).not.toContain('not considering anything');
  });

  it('shows the product\'s refusal instead of a generic failure', async () => {
    stubFetch({
      refusal: { code: 'instrument_not_carried', message: 'This product does not carry that instrument.' },
    });
    const wrapper = mount(IntentsView, { global });
    await flushPromises();
    await wrapper.findComponent({ name: 'IntentForm' }).vm.$emit('submit', {
      instrumentId: '33333333-3333-4333-8333-333333333333',
      direction: 'buy', quantity: '10', price: '10', costs: '0',
    });
    await flushPromises();
    expect(wrapper.text()).toContain('This product does not carry that instrument.');
  });

  it('settles through the write path, with the status the person chose', async () => {
    const wrapper = mount(IntentsView, { global });
    await flushPromises();
    await wrapper.findComponent({ name: 'IntentList' }).vm.$emit('settle',
      '11111111-1111-4111-8111-111111111111', 'acted_on');
    await flushPromises();

    const settle = calls.find((call) => call.method === 'PATCH');
    expect(settle?.url).toBe('/api/v1/order-intents/11111111-1111-4111-8111-111111111111');
    expect(settle?.body).toContain('acted_on');
  });

  // FR-011: marking one acted on creates no trade. The screen must not quietly post one either.
  it('records no trade when an intent is marked acted on', async () => {
    const wrapper = mount(IntentsView, { global });
    await flushPromises();
    await wrapper.findComponent({ name: 'IntentList' }).vm.$emit('settle',
      '11111111-1111-4111-8111-111111111111', 'acted_on');
    await flushPromises();

    expect(calls.filter((call) => call.url.includes('/portfolio/trades') && call.method !== 'GET'))
      .toHaveLength(0);
    expect(wrapper.text().toLowerCase()).toContain('does not record a trade');
  });
});
