import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import HoldingTable from './HoldingTable.vue';
import type { PortfolioHolding } from '@/types/marketData';

function holding(overrides: Partial<PortfolioHolding> = {}): PortfolioHolding {
  return {
    instrumentId: '44000000-0000-4000-8000-000000000001',
    ticker: 'VOLV-B',
    name: 'Volvo AB',
    currency: 'SEK',
    quantity: '100.000000000000',
    cost: '24619.000000000000',
    valuation: {
      value: '28400.000000000000', session: '2026-09-16',
      conversionRate: null, absenceReason: null,
    },
    unrealised: '3781.000000000000',
    comparison: {
      series: 'OMXS30.INDX', fromSession: '2026-03-16', toSession: '2026-09-16',
      holdingReturn: '0.153600000000', benchmarkReturn: '0.082000000000', absenceReason: null,
    },
    ...overrides,
  };
}

describe('HoldingTable', () => {
  // The rule that matters most here: a reader who cannot distinguish red from green must not get
  // the opposite of the truth about their own money.
  it('says whether a holding is up or down in words, not only in colour', () => {
    const wrapper = mount(HoldingTable, {
      props: {
        currency: 'SEK',
        holdings: [
          holding(),
          holding({ ticker: 'NOKIA', unrealised: '-1200.000000000000' }),
        ],
      },
    });
    const text = wrapper.text();
    expect(text).toContain('Up');
    expect(text).toContain('Down');
    // And the figures themselves are present as text.
    expect(text).toContain('3,781');
    expect(text).toContain('1,200');
  });

  it('says which session priced each holding', () => {
    const wrapper = mount(HoldingTable, { props: { currency: 'SEK', holdings: [holding()] } });
    // A multi-market portfolio is valued at slightly different sessions; hiding that inside one
    // total would be the dishonest option.
    expect(wrapper.text()).toContain('2026-09-16');
  });

  it('states why a holding could not be valued rather than showing nothing', () => {
    const wrapper = mount(HoldingTable, {
      props: {
        currency: 'SEK',
        holdings: [holding({
          valuation: { value: null, session: null, conversionRate: null, absenceReason: 'no_price' },
          unrealised: null,
        })],
      },
    });
    const text = wrapper.text();
    expect(text).toContain('Not valued');
    expect(text).toContain('no stored price');
  });

  it('states why a comparison is unavailable rather than omitting it', () => {
    const wrapper = mount(HoldingTable, {
      props: {
        currency: 'SEK',
        holdings: [holding({
          comparison: {
            series: 'OMXS30.INDX', fromSession: null, toSession: null,
            holdingReturn: null, benchmarkReturn: null,
            absenceReason: 'series_does_not_cover_window',
          },
        })],
      },
    });
    expect(wrapper.text()).toContain('does not reach back to when you bought');
  });

  it('shows the market beside the holding, so a gain is never read alone', () => {
    const wrapper = mount(HoldingTable, { props: { currency: 'SEK', holdings: [holding()] } });
    const text = wrapper.text();
    expect(text).toContain('15.36%');
    expect(text).toContain('OMXS30.INDX');
    expect(text).toContain('8.20%');
  });

  it('says what to do when nothing is recorded yet', () => {
    const wrapper = mount(HoldingTable, { props: { currency: 'SEK', holdings: [] } });
    expect(wrapper.text()).toContain('not recorded anything yet');
  });
});
