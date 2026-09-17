import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import LimitTable from './LimitTable.vue';
import type { LimitEvaluation } from '@/types/marketData';

function limit(overrides: Partial<LimitEvaluation> = {}): LimitEvaluation {
  return {
    kind: 'instrument_share',
    threshold: '0.250000000000',
    state: 'exceeded',
    measured: '0.412300000000',
    denominator: '284000.000000000000',
    absenceReason: null,
    contributions: [
      { label: 'VOLV-B', value: '117093.200000000000', share: '0.412300000000' },
      { label: 'NOKIA', value: '166906.800000000000', share: '0.587700000000' },
    ],
    ...overrides,
  };
}

describe('LimitTable', () => {
  // A person who cannot tell red from green must not have to guess whether they are inside their
  // own limit.
  it('says the state in words rather than only in colour', () => {
    const wrapper = mount(LimitTable, {
      props: {
        currency: 'SEK',
        limits: [
          limit(),
          limit({ kind: 'sector_share', state: 'within', measured: '0.310000000000' }),
          limit({
            kind: 'market_share', state: 'unevaluable', measured: null, denominator: null,
            absenceReason: 'portfolio_incomplete', contributions: [],
          }),
        ],
      },
    });
    const text = wrapper.text();
    expect(text).toContain('Over');
    expect(text).toContain('Within');
    expect(text).toContain('Not measured');
  });

  it('shows the arithmetic, so a percentage can be checked rather than believed', () => {
    const wrapper = mount(LimitTable, { props: { currency: 'SEK', limits: [limit()] } });
    const text = wrapper.text();
    expect(text).toContain('41.2%');
    expect(text).toContain('25.0%');
    // The total it divided by, and the breakdown that produced it.
    expect(text).toContain('284,000');
    expect(text).toContain('VOLV-B');
    expect(text).toContain('NOKIA');
  });

  it('states the gap without saying what would close it', () => {
    const wrapper = mount(LimitTable, { props: { currency: 'SEK', limits: [limit()] } });
    const text = wrapper.text().toLowerCase();
    // 41.2 − 25.0 = 16.2 points.
    expect(text).toContain('16.2 points over');
    // FR-015: no action, no quantity, no amount to move.
    for (const forbidden of ['sell', 'reduce', 'suggest', 'recommend', 'should']) {
      expect(text).not.toContain(forbidden);
    }
  });

  it('states why an unevaluable limit could not be measured', () => {
    const wrapper = mount(LimitTable, {
      props: {
        currency: 'SEK',
        limits: [limit({
          state: 'unevaluable', measured: null, denominator: null,
          absenceReason: 'portfolio_incomplete', contributions: [],
        })],
      },
    });
    expect(wrapper.text()).toContain('could not be priced');
  });

  it('tells a person with no limits that the product suggests none', () => {
    const wrapper = mount(LimitTable, { props: { currency: 'SEK', limits: [] } });
    const text = wrapper.text();
    expect(text).toContain('have not set any limits');
    expect(text).toContain('does not suggest any');
  });

  it('reads a holding count as a count rather than a percentage', () => {
    const wrapper = mount(LimitTable, {
      props: {
        currency: 'SEK',
        limits: [limit({
          kind: 'holding_count', threshold: '15.000000000000', state: 'within',
          measured: '7.000000000000', denominator: null, contributions: [],
        })],
      },
    });
    const text = wrapper.text();
    expect(text).toContain('7');
    expect(text).not.toContain('700.0%');
  });
});
