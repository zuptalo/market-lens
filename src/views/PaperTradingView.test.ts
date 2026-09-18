import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import PaperTradingView from './PaperTradingView.vue';

class QuietEventSource extends EventTarget {
  close(): void {}
}

function wireAccount(overrides: Record<string, unknown> = {}) {
  return {
    starting_cash: '1000000.000000000000',
    accounting_currency: 'SEK',
    opened_at: '2026-06-01T08:00:00Z',
    costs: { brokerage_bps: '10', brokerage_minimum: '5', slippage_bps: '5', currency_spread_bps: '10' },
    cash: '727440.000000000000',
    holdings: [],
    orders: [{
      id: 'o1', intent_id: 'i1', instrument_id: '33333333-3333-4333-8333-333333333333',
      ticker: 'ABB', name: 'ABB Ltd', currency: 'SEK', direction: 'buy',
      quantity: '400.000000000000', expected_price: '600.000000000000',
      placed_session: '2026-09-16', state: 'pending', absence_reason: null,
      placed_at: '2026-09-16T18:00:00Z', settled_at: null, fill: null,
    }],
    total: {
      value: '0.000000000000', cost: '0.000000000000', unrealised: '0.000000000000',
      realised: '0.000000000000', total_return: '0.000000000000',
      complete: true, incomplete_reason: null,
    },
    is_a_simulation: true,
    ...overrides,
  };
}

let calls: { url: string; method: string; body: string }[] = [];

function stubFetch(options: { account?: unknown; status?: number; intents?: unknown[] } = {}) {
  calls = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    calls.push({ url, method, body: String(init?.body ?? '') });
    if (url.includes('/api/v1/paper-account')) {
      if (options.status === 404 && method === 'GET') {
        return { ok: false, status: 404, json: async () => ({ error: 'no_account' }) };
      }
      return { ok: true, status: 200, json: async () => options.account ?? wireAccount() };
    }
    if (url.includes('/api/v1/order-intents')) {
      return { ok: true, json: async () => ({
        intents: options.intents ?? [{
          id: 'i2', instrument_id: '33333333-3333-4333-8333-333333333333', ticker: 'VOLV-B',
          name: 'Volvo B', currency: 'SEK', direction: 'buy', quantity: '50.000000000000',
          price: '284.100000000000', costs: '0.000000000000', status: 'considering',
          recorded_at: '2026-09-17T09:00:00Z', settled_at: null, consequence: null,
        }],
        evaluated_independently: true, records_what_you_are_considering: true,
      }) };
    }
    return { ok: false, status: 404, json: async () => ({ error: 'not found' }) };
  }));
}

const global = { plugins: [PrimeVue] };

describe('PaperTradingView', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  it('says the account is a simulation and that nothing was traded', async () => {
    const wrapper = mount(PaperTradingView, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('simulation');
    expect(text).toContain('nothing here was traded');
    for (const forbidden of ['recommend', 'you should', 'suggested', 'proven strategy']) {
      expect(text).not.toContain(forbidden);
    }
  });

  it('reads nothing about anybody else — no path carries a user', async () => {
    mount(PaperTradingView, { global });
    await flushPromises();
    const read = calls.find((call) => call.url.includes('/api/v1/paper-account'));
    expect(read?.url).toBe('/api/v1/paper-account');
  });

  /**
   * A person who has not opened one is offered to, rather than shown an error. Opening it is the
   * only moment its terms can be chosen, so the screen says that too.
   */
  it('offers to open an account when there is none, and says the terms are final', async () => {
    stubFetch({ status: 404 });
    const wrapper = mount(PaperTradingView, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('open a paper account');
    expect(text).toContain('cannot be changed');
    expect(wrapper.find('[data-testid="paper-order-list"]').exists()).toBe(false);
  });

  it('opens the account with what the person stated', async () => {
    stubFetch({ status: 404 });
    const wrapper = mount(PaperTradingView, { global });
    await flushPromises();

    const numbers = wrapper.findAllComponents({ name: 'InputNumber' });
    await numbers[0].setValue(1000000);
    await wrapper.find('form').trigger('submit');
    await flushPromises();

    const opened = calls.find((call) => call.method === 'POST' && call.url.endsWith('/paper-account'));
    expect(opened?.body).toContain('1000000');
    expect(opened?.body).toContain('accounting_currency');
  });

  it('promotes only what the person chose, through the write path', async () => {
    const wrapper = mount(PaperTradingView, { global });
    await flushPromises();
    await wrapper.findComponent({ name: 'PaperPromoteForm' }).vm.$emit('promote', 'i2');
    await flushPromises();

    const promoted = calls.find((call) => call.url.endsWith('/paper-account/orders'));
    expect(promoted?.method).toBe('POST');
    expect(promoted?.body).toContain('i2');
  });

  it('withdraws a pending order through the write path', async () => {
    const wrapper = mount(PaperTradingView, { global });
    await flushPromises();
    await wrapper.findComponent({ name: 'PaperOrderList' }).vm.$emit('cancel', 'o1');
    await flushPromises();

    const cancelled = calls.find((call) => call.method === 'DELETE');
    expect(cancelled?.url).toBe('/api/v1/paper-account/orders/o1');
  });

  it('reports a failure rather than an empty account', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false, status: 500, json: async () => ({ error: 'unavailable' }),
    })));
    const wrapper = mount(PaperTradingView, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('unable to load');
    expect(text).not.toContain('holding nothing');
  });

  // FR-020: no screen combines a paper figure with a real holding into one number.
  it('reads no real portfolio at all', async () => {
    mount(PaperTradingView, { global });
    await flushPromises();
    expect(calls.filter((call) => call.url.includes('/api/v1/portfolio'))).toHaveLength(0);
  });
});
