import { describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import EquityCurve from './EquityCurve.vue';
import type { BacktestEquityPoint } from '@/types/marketData';

vi.mock('lightweight-charts', () => ({
  AreaSeries: {},
  createChart: () => ({
    addSeries: () => ({ setData: () => {} }),
    timeScale: () => ({ fitContent: () => {} }),
    remove: () => {},
  }),
}));

function point(overrides: Partial<BacktestEquityPoint> = {}): BacktestEquityPoint {
  return {
    sessionDate: '2026-09-14',
    cash: '505855.000000000000',
    positionsValue: '912345.000000000000',
    total: '1418200.000000000000',
    absenceReason: null,
    ...overrides,
  };
}

describe('EquityCurve', () => {
  // The requirement the chart exists under: everything the shape conveys is also text. A reader
  // who cannot see a canvas receives the result, not a poorer version of it.
  it('states every figure the chart draws as text', () => {
    const wrapper = mount(EquityCurve, {
      props: {
        currency: 'EUR',
        points: [
          point({ sessionDate: '2026-01-02', total: '1000000.000000000000' }),
          point({ sessionDate: '2026-05-04', total: '1500000.000000000000' }),
          point({ sessionDate: '2026-07-01', total: '900000.000000000000' }),
          point({ sessionDate: '2026-09-14', total: '1418200.000000000000' }),
        ],
      },
    });
    const text = wrapper.text();
    expect(text).toContain('Started');
    expect(text).toContain('Ended');
    expect(text).toContain('Highest');
    expect(text).toContain('Lowest');
    expect(text).toContain('2026-01-02');
    expect(text).toContain('2026-05-04');
    expect(text).toContain('2026-07-01');
    expect(wrapper.find('[data-testid="equity-figures"]').exists()).toBe(true);
  });

  // FR-011. A session that could not be valued is counted and explained, never bridged into the
  // curve as though the portfolio had simply held its value.
  it('reports the sessions it could not value', () => {
    const wrapper = mount(EquityCurve, {
      props: {
        currency: 'EUR',
        points: [
          point({ sessionDate: '2026-09-14' }),
          point({
            sessionDate: '2026-09-15', positionsValue: null, total: null,
            absenceReason: 'position_unvalued',
          }),
        ],
      },
    });
    const text = wrapper.text();
    expect(text).toContain('could not be valued');
    expect(text).toContain('1');
  });

  it('says so when nothing in the range could be valued', () => {
    const wrapper = mount(EquityCurve, {
      props: {
        currency: 'EUR',
        points: [point({ positionsValue: null, total: null, absenceReason: 'position_unvalued' })],
      },
    });
    expect(wrapper.text()).toContain('no curve to show');
    expect(wrapper.find('[data-testid="equity-chart"]').exists()).toBe(false);
  });
});
