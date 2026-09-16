import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import MeasureTable from './MeasureTable.vue';
import type { BacktestBenchmark, BacktestMeasures } from '@/types/marketData';

function measures(overrides: Partial<BacktestMeasures> = {}): BacktestMeasures {
  return {
    fromSession: '2016-08-31',
    toSession: '2026-09-15',
    totalReturn: '0.418200000000',
    annualisedReturn: '0.035600000000',
    volatility: '0.184300000000',
    maximumDrawdown: '-0.312400000000',
    tradeCount: 312,
    totalCosts: '18849.571698069468',
    absenceReason: null,
    ...overrides,
  };
}

function benchmark(overrides: Partial<BacktestBenchmark> = {}): BacktestBenchmark {
  return {
    mic: 'XSTO',
    series: 'OMXS30.INDX',
    measures: measures({ totalReturn: '0.612000000000', tradeCount: 0, totalCosts: '0' }),
    absenceReason: null,
    ...overrides,
  };
}

describe('MeasureTable', () => {
  // FR-018. The two figures that flatter least are exactly the two a result would leave out, so
  // their presence is asserted rather than trusted.
  it('shows all six measures, including the unflattering ones', () => {
    const wrapper = mount(MeasureTable, {
      props: { measures: measures(), benchmarks: [benchmark()], currency: 'EUR' },
    });
    const text = wrapper.text();
    expect(text).toContain('Total return');
    expect(text).toContain('Annualised return');
    expect(text).toContain('Volatility');
    expect(text).toContain('Maximum drawdown');
    expect(text).toContain('Trades');
    expect(text).toContain('Cost of trading');
    // The figures themselves, as text. A drawdown shown only as a shape on a chart is not shown.
    expect(text).toContain('41.82%');
    expect(text).toContain('-31.24%');
    expect(text).toContain('312');
  });

  it('shows the benchmark beside the strategy over the same range', () => {
    const wrapper = mount(MeasureTable, {
      props: { measures: measures(), benchmarks: [benchmark()], currency: 'EUR' },
    });
    const text = wrapper.text();
    expect(text).toContain('OMXS30.INDX');
    expect(text).toContain('61.20%');
  });

  // FR-020. The Danish index begins after this product's stored history. Saying so is the whole
  // behaviour: a comparison quietly measured over a shorter window would look like evidence.
  it('states why a comparison is unavailable rather than showing a shorter one', () => {
    const wrapper = mount(MeasureTable, {
      props: {
        measures: measures(),
        benchmarks: [benchmark({
          mic: 'XCSE',
          series: 'OMXC25.INDX',
          measures: measures({
            fromSession: null, toSession: null, totalReturn: null, annualisedReturn: null,
            volatility: null, maximumDrawdown: null, tradeCount: null, totalCosts: null,
            absenceReason: 'series_starts_after_range',
          }),
          absenceReason: 'series_starts_after_range',
        })],
        currency: 'EUR',
      },
    });
    const text = wrapper.text();
    expect(text).toContain('OMXC25.INDX');
    expect(text).toContain('begins after this backtest does');
    // And no figure is invented for it.
    expect(text).not.toContain('61.20%');
  });

  it('says plainly when the result itself could not be measured', () => {
    const wrapper = mount(MeasureTable, {
      props: {
        measures: measures({
          totalReturn: null, annualisedReturn: null, volatility: null, maximumDrawdown: null,
          tradeCount: null, totalCosts: null, absenceReason: 'insufficient_sessions',
        }),
        benchmarks: [],
        currency: 'EUR',
      },
    });
    expect(wrapper.text()).toContain('too few sessions');
  });

  // A benchmark is context, not a verdict. Nothing in the table carries direction by colour
  // alone: the sign is in the number a reader can read aloud.
  it('carries every direction in text rather than in colour', () => {
    const wrapper = mount(MeasureTable, {
      props: { measures: measures(), benchmarks: [benchmark()], currency: 'EUR' },
    });
    expect(wrapper.text()).toContain('-31.24%');
    expect(wrapper.html()).not.toMatch(/color:\s*(red|green)/i);
  });
});
