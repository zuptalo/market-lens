import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import PaperOrderList from './PaperOrderList.vue';
import type { PaperOrder } from '@/types/marketData';

function order(overrides: Partial<PaperOrder> = {}): PaperOrder {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    intentId: '99999999-9999-4999-8999-999999999999',
    instrumentId: '33333333-3333-4333-8333-333333333333',
    ticker: 'ABB',
    name: 'ABB Ltd',
    currency: 'SEK',
    direction: 'buy',
    quantity: '400.000000000000',
    expectedPrice: '600.000000000000',
    placedSession: '2026-09-16',
    state: 'filled',
    absenceReason: null,
    placedAt: '2026-09-16T18:00:00Z',
    settledAt: '2026-09-17T06:00:00Z',
    fill: {
      fillSession: '2026-09-17',
      openPrice: '600.300000000000',
      quantity: '400.000000000000',
      costs: '252.120000000000',
      cashEffect: '-240372.120000000000',
      conversionRate: '1.000000000000',
      barDiverged: false,
      filledAt: '2026-09-17T06:00:00Z',
    },
    ...overrides,
  };
}

describe('PaperOrderList', () => {
  it('shows what was expected beside what was actually paid', () => {
    const wrapper = mount(PaperOrderList, { props: { orders: [order()], currency: 'SEK' } });
    const text = wrapper.text();
    expect(text).toContain('ABB');
    // The price expected, and the open it actually filled at.
    expect(text).toContain('600.00');
    expect(text).toContain('600.30');
    // And the session it filled at, which is not the one it was placed in.
    expect(text).toContain('2026-09-17');
  });

  it('writes prices as prices, not as stored decimals', () => {
    const wrapper = mount(PaperOrderList, { props: { orders: [order()], currency: 'SEK' } });
    expect(wrapper.text()).not.toContain('600.300000000000');
    expect(wrapper.text()).not.toContain('400.000000000000');
  });

  // The four states are carried by a word, never by colour alone.
  it('says each state in words', () => {
    const wrapper = mount(PaperOrderList, {
      props: {
        currency: 'SEK',
        orders: [
          order({ id: 'a', state: 'pending', settledAt: null, fill: null }),
          order({ id: 'b', state: 'filled' }),
          order({ id: 'c', state: 'cancelled', fill: null }),
          order({ id: 'd', state: 'unfillable', absenceReason: 'insufficient_cash', fill: null }),
        ],
      },
    });
    const text = wrapper.text();
    expect(text).toContain('Waiting');
    expect(text).toContain('Filled');
    expect(text).toContain('Withdrawn');
    expect(text).toContain('Could not fill');
  });

  it('says why an order could not fill, in words a person can act on', () => {
    for (const [reason, said] of [
      ['insufficient_cash', 'not enough cash'],
      ['exceeds_position', 'more than the account held'],
      ['no_price', 'no price'],
    ] as const) {
      const wrapper = mount(PaperOrderList, {
        props: {
          currency: 'SEK',
          orders: [order({ state: 'unfillable', absenceReason: reason, fill: null })],
        },
      });
      expect(wrapper.text().toLowerCase()).toContain(said);
    }
  });

  // A pending order has not been priced yet, and the screen says what will happen rather than
  // leaving a blank that reads as a failure.
  it('says a waiting order fills at the next session open', () => {
    const wrapper = mount(PaperOrderList, {
      props: {
        currency: 'SEK',
        orders: [order({ state: 'pending', settledAt: null, fill: null })],
      },
    });
    expect(wrapper.text().toLowerCase()).toContain('next session');
  });

  // A corrected bar is reported, never re-priced: that is what keeps the account reconcilable.
  it('says when the price behind a fill has since been corrected', () => {
    const wrapper = mount(PaperOrderList, {
      props: {
        currency: 'SEK',
        orders: [order({ fill: { ...order().fill!, barDiverged: true } })],
      },
    });
    expect(wrapper.text().toLowerCase()).toContain('has since been corrected');
  });

  it('offers to withdraw only what is still waiting', () => {
    const wrapper = mount(PaperOrderList, {
      props: {
        currency: 'SEK',
        orders: [
          order({ id: 'a', state: 'pending', settledAt: null, fill: null }),
          order({ id: 'b', state: 'filled' }),
        ],
      },
    });
    const withdraw = wrapper.findAll('button').filter((b) => /withdraw/i.test(b.text()));
    expect(withdraw).toHaveLength(1);
  });

  it('emits the order the person withdrew', async () => {
    const wrapper = mount(PaperOrderList, {
      props: {
        currency: 'SEK',
        orders: [order({ id: 'a', state: 'pending', settledAt: null, fill: null })],
      },
    });
    await wrapper.findAll('button').find((b) => /withdraw/i.test(b.text()))?.trigger('click');
    expect(wrapper.emitted('cancel')?.[0]).toEqual(['a']);
  });

  it('never tells the person what to do next', () => {
    const wrapper = mount(PaperOrderList, { props: { orders: [order()], currency: 'SEK' } });
    const text = wrapper.text().toLowerCase();
    for (const forbidden of ['recommend', 'suggest', 'you should', 'place an order', 'buy more']) {
      expect(text).not.toContain(forbidden);
    }
  });

  it('says when nothing has been promoted rather than showing an empty table', () => {
    const wrapper = mount(PaperOrderList, { props: { orders: [], currency: 'SEK' } });
    expect(wrapper.text().toLowerCase()).toContain('nothing promoted');
  });
});
