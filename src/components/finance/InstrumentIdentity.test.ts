import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import InstrumentIdentity from './InstrumentIdentity.vue';

const instrument = {
  id: '33000000-0000-4000-8000-000000000001',
  isin: 'SE0000000100',
  ticker: 'ALFA',
  name: 'Alpha AB',
  exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm', timezone: 'Europe/Stockholm' },
  currency: 'SEK',
  country: 'SE',
  instrumentType: 'common_stock' as const,
  active: true,
  purchasabilityStatus: 'unverified' as const,
};

describe('InstrumentIdentity', () => {
  it('renders exchange-qualified identity without relying on ticker alone', () => {
    const wrapper = mount(InstrumentIdentity, { props: { instrument } });
    expect(wrapper.get('h2').text()).toContain('Alpha AB');
    expect(wrapper.text()).toContain('ALFA · XSTO');
    expect(wrapper.text()).toContain('SE0000000100');
    expect(wrapper.text()).toContain('SEK');
  });

  it('labels stored daily data as latest-known with session, native decimals, coverage, freshness, and warnings', () => {
    const wrapper = mount(InstrumentIdentity, { props: {
      instrument,
      latestBar: { sessionDate: '2026-08-28', open: '100.125', high: '102.5', low: '99.75', close: '101.25', adjustedClose: null, volume: 1234, currency: 'SEK', provider: 'fixture', observedAt: '2026-08-29T18:30:00Z' },
      history: { firstSession: '2026-08-27', lastSession: '2026-08-28', barCount: 2 },
      qualitySummary: { openWarnings: 1, openErrors: 0 },
    } });
    expect(wrapper.text()).toContain('Latest known daily value');
    expect(wrapper.text()).toContain('Session 2026-08-28');
    expect(wrapper.text()).toContain('101.25 SEK');
    expect(wrapper.text()).toContain('2026-08-27 to 2026-08-28');
    expect(wrapper.get('[data-testid="quality-summary"]').text()).toContain('1 open warning');
    expect(wrapper.text()).not.toContain('Real-time');
  });

  it('shows an explicit empty-history state', () => {
    const wrapper = mount(InstrumentIdentity, { props: {
      instrument,
      latestBar: null,
      history: { firstSession: null, lastSession: null, barCount: 0 },
      qualitySummary: { openWarnings: 0, openErrors: 0 },
    } });
    expect(wrapper.get('[role="status"]').text()).toContain('No daily history is available');
  });
});
