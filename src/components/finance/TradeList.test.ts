import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import { RouterLinkStub } from '@vue/test-utils';
import TradeList from './TradeList.vue';
import type { BacktestTrade } from '@/types/marketData';

function trade(overrides: Partial<BacktestTrade> = {}): BacktestTrade {
  return {
    id: '66000000-0000-4000-8000-000000000001',
    instrumentId: '44000000-0000-4000-8000-000000000001',
    ticker: 'VOLV-B',
    name: 'Volvo AB',
    signalSession: '2026-09-01',
    executionSession: '2026-09-02',
    direction: 'buy',
    quantity: '100.000000000000',
    price: '42.500000000000',
    currency: 'SEK',
    conversionRate: '9.456700000000',
    brokerage: '5.000000000000',
    slippage: '0.224700000000',
    currencySpread: '0.449500000000',
    cashEffect: '-455.000000000000',
    signalId: '44000000-0000-4000-8000-000000000001/2026-09-01/00000000-0015-4000-8000-000000000001',
    ...overrides,
  };
}

const global = { stubs: { RouterLink: RouterLinkStub } };

describe('TradeList', () => {
  // SC-003. A trade a reader cannot question is an assertion, so every row leads to the signal
  // behind it — and it does so by an identifier nobody has to know.
  it('leads from every trade to the signal that caused it', () => {
    const wrapper = mount(TradeList, { props: { trades: [trade()] }, global });
    const link = wrapper.findComponent(RouterLinkStub);
    expect(link.exists()).toBe(true);
    expect(String(link.props('to'))).toContain('44000000-0000-4000-8000-000000000001');
    expect(String(link.props('to'))).toContain('2026-09-01');
    // And it is reachable by keyboard with a label that says what it leads to.
    expect(link.attributes('aria-label')).toContain('VOLV-B');
  });

  // The session a trade executed on, and the session whose signal caused it, are both shown.
  // A reader who cannot see both cannot tell whether the simulation read the future.
  it('shows both the execution session and the signal session', () => {
    const wrapper = mount(TradeList, { props: { trades: [trade()] }, global });
    const text = wrapper.text();
    expect(text).toContain('2026-09-02');
    expect(text).toContain('2026-09-01');
  });

  it('names the direction in words rather than by colour', () => {
    const wrapper = mount(TradeList, {
      props: { trades: [trade(), trade({ id: 'x', direction: 'sell' })] },
      global,
    });
    const text = wrapper.text();
    expect(text).toContain('Bought');
    expect(text).toContain('Sold');
  });

  it('shows what each trade cost', () => {
    const wrapper = mount(TradeList, { props: { trades: [trade()] }, global });
    // Brokerage, slippage and spread together: 5 + 0.2247 + 0.4495.
    expect(wrapper.text()).toContain('5.67');
  });

  // A range that produced no trade is a result, not a failure, and reads as one.
  it('states plainly when the rules never traded', () => {
    const wrapper = mount(TradeList, { props: { trades: [] }, global });
    expect(wrapper.text()).toContain('made no trade');
    expect(wrapper.text()).toContain('That is a result');
  });
});
