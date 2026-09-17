import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import TradeHistory from './TradeHistory.vue';
import type { PortfolioTrade } from '@/types/marketData';

function trade(overrides: Partial<PortfolioTrade> = {}): PortfolioTrade {
  return {
    id: '88000000-0022-4000-8000-000000000001',
    instrumentId: '44000000-0000-4000-8000-000000000001',
    ticker: 'VOLV-B',
    name: 'Volvo AB',
    direction: 'buy',
    quantity: '100.000000000000',
    price: '245.800000000000',
    currency: 'SEK',
    costs: '39.000000000000',
    tradeDate: '2026-03-16',
    sequence: 1,
    status: 'current',
    supersedes: null,
    recordedAt: '2026-03-16T09:00:00Z',
    changedAt: null,
    ...overrides,
  };
}

describe('TradeHistory', () => {
  // A superseded entry stays visible because that is what makes a portfolio reconcilable: somebody
  // checking last month's figure against a broker statement needs the version that produced it.
  it('keeps replaced and withdrawn entries readable, marked for what they are', () => {
    const wrapper = mount(TradeHistory, {
      props: {
        showSuperseded: true,
        trades: [
          trade({ changedAt: '2026-04-01T09:00:00Z' }),
          trade({ id: 'b', status: 'superseded' }),
          trade({ id: 'c', status: 'withdrawn' }),
        ],
      },
    });
    const text = wrapper.text();
    expect(text).toContain('Counts');
    expect(text).toContain('Replaced');
    expect(text).toContain('Withdrawn');
    expect(text).toContain('kept for reference');
  });

  // The entry order decides which shares first-in-first-out consumes, so it is shown rather than
  // hidden — two trades on one date are otherwise indistinguishable.
  it('shows the order a trade was recorded in', () => {
    const wrapper = mount(TradeHistory, { props: { trades: [trade({ sequence: 7 })] } });
    expect(wrapper.text()).toContain('entry 7');
  });

  it('offers correction and withdrawal only on the version that counts', () => {
    const wrapper = mount(TradeHistory, {
      props: { showSuperseded: true, trades: [trade(), trade({ id: 'b', status: 'superseded' })] },
    });
    // One row has both actions; the superseded row has neither.
    const labels = wrapper.findAll('button').map((button) => button.text());
    expect(labels.filter((label) => label === 'Correct')).toHaveLength(1);
    expect(labels.filter((label) => label === 'Withdraw')).toHaveLength(1);
  });

  it('names the direction in words rather than by colour', () => {
    const wrapper = mount(TradeHistory, {
      props: { trades: [trade(), trade({ id: 'b', direction: 'sell' })] },
    });
    expect(wrapper.text()).toContain('Bought');
    expect(wrapper.text()).toContain('Sold');
  });

  it('shows what a trade cost when it cost something', () => {
    const wrapper = mount(TradeHistory, { props: { trades: [trade()] } });
    expect(wrapper.text()).toContain('39');
  });
});
