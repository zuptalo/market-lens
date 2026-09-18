import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import PaperAccountSummary from './PaperAccountSummary.vue';
import type { PaperAccount } from '@/types/marketData';

function account(overrides: Partial<PaperAccount> = {}): PaperAccount {
  return {
    startingCash: '1000000.000000000000',
    accountingCurrency: 'SEK',
    openedAt: '2026-06-01T08:00:00Z',
    costs: {
      brokerageBps: '10', brokerageMinimum: '5', slippageBps: '5', currencySpreadBps: '10',
    },
    cash: '727440.000000000000',
    holdings: [{
      instrumentId: '33333333-3333-4333-8333-333333333333',
      ticker: 'ABB', name: 'ABB Ltd', currency: 'SEK',
      quantity: '400.000000000000', cost: '240120.000000000000',
      value: '272560.000000000000', unrealised: '32440.000000000000',
      session: '2026-09-17', absenceReason: null,
      comparison: {
        series: 'OMXS30.INDX', holdingReturn: '0.135600000000',
        benchmarkReturn: '0.092100000000', absenceReason: null,
      },
    }],
    orders: [],
    totals: {
      value: '272560.000000000000', cost: '240120.000000000000',
      unrealised: '32440.000000000000', realised: '2410.000000000000',
      totalReturn: '0.043210000000', complete: true, incompleteReason: null,
    },
    isASimulation: true,
    ...overrides,
  };
}

describe('PaperAccountSummary', () => {
  /**
   * The statement the whole screen is built around. A simulated account that grows looks exactly
   * like a recommendation to do the same with real money, and this is the one screen where that
   * confusion costs something.
   */
  it('says plainly that nothing here was traded', () => {
    const text = mount(PaperAccountSummary, { props: { account: account() } }).text().toLowerCase();
    expect(text).toContain('simulation');
    expect(text).toContain('nothing here was traded');
    for (const forbidden of ['recommend', 'suggest', 'you should', 'proven', 'guaranteed']) {
      expect(text).not.toContain(forbidden);
    }
  });

  it('reports what the account is worth and what it has returned', () => {
    const text = mount(PaperAccountSummary, { props: { account: account() } }).text();
    expect(text).toContain('727,440');   // cash
    expect(text).toContain('272,560');   // holdings
    expect(text).toContain('1,000,000'); // where it started
    expect(text).toContain('4.3%');      // the return
  });

  /**
   * The one place in the product that reports a return, and it says why it may — otherwise it reads
   * as an inconsistency with the real portfolio, which declines to state one.
   */
  it('says why a return is honest here', () => {
    const text = mount(PaperAccountSummary, { props: { account: account() } }).text().toLowerCase();
    expect(text).toContain('every movement');
  });

  it('states the costs it charged rather than hiding them', () => {
    const text = mount(PaperAccountSummary, { props: { account: account() } }).text();
    expect(text).toContain('10');
    expect(text.toLowerCase()).toContain('brokerage');
  });

  // Understating a total would be a quiet, flattering lie about a loss that never happened.
  it('declines to state a total it cannot complete, and says why', () => {
    const wrapper = mount(PaperAccountSummary, {
      props: {
        account: account({
          totals: {
            value: null, cost: '240120.000000000000', unrealised: null,
            realised: '0.000000000000', totalReturn: null,
            complete: false, incompleteReason: 'no_price',
          },
        }),
      },
    });
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('could not be priced');
    expect(text).not.toContain('0.0%');
  });

  it('shows each holding against the market it trades in', () => {
    const text = mount(PaperAccountSummary, { props: { account: account() } }).text();
    expect(text).toContain('ABB');
    expect(text).toContain('13.6%');
    expect(text).toContain('9.2%');
    expect(text).toContain('OMXS30.INDX');
  });

  it('says when nothing is held yet', () => {
    const wrapper = mount(PaperAccountSummary, {
      props: {
        account: account({
          holdings: [],
          totals: {
            value: '0.000000000000', cost: '0.000000000000', unrealised: '0.000000000000',
            realised: '0.000000000000', totalReturn: '0.000000000000',
            complete: true, incompleteReason: null,
          },
        }),
      },
    });
    expect(wrapper.text().toLowerCase()).toContain('holding nothing');
  });
});
