import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import ContributionList from './ContributionList.vue';
import type { SignalContribution } from '@/types/marketData';

function contribution(overrides: Partial<SignalContribution> = {}): SignalContribution {
  return {
    factor: 'momentum_90',
    feature: 'return_90',
    featureValue: '0.081234000000',
    featureSession: '2026-06-30',
    factorScore: '0.400000000000',
    weight: '0.250000000000',
    contribution: '0.100000000000',
    unavailableReason: null,
    ...overrides,
  };
}

describe('ContributionList', () => {
  // SC-010. Direction and magnitude must be readable as text, because a screen reader gets
  // nothing from a green bar and a person who cannot distinguish red from green gets the
  // opposite of the truth. Colour may accompany the text; it may never be the only carrier.
  it('states every contribution as text a screen reader can read', () => {
    const wrapper = mount(ContributionList, {
      props: { contributions: [contribution(), contribution({
        factor: 'volatility_penalty', feature: 'volatility_20', featureValue: '0.480000000000',
        factorScore: '-0.466666666667', weight: '0.100000000000', contribution: '-0.046666666667',
      })] },
    });
    const text = wrapper.text();
    expect(text).toContain('momentum_90');
    expect(text).toContain('return_90');
    expect(text).toContain('volatility_penalty');
    // The direction as a word, not only as a sign or a colour.
    expect(text.toLowerCase()).toContain('raises');
    expect(text.toLowerCase()).toContain('lowers');
    // The magnitude as a number in the text.
    expect(text).toContain('0.100');
    expect(text).toContain('0.047');
    expect(text).toContain('0.25');
  });

  it('says why a factor contributed nothing rather than showing it as zero', () => {
    const wrapper = mount(ContributionList, {
      props: { contributions: [contribution({
        factor: 'regime', feature: 'regime', featureValue: null, factorScore: null,
        contribution: null, unavailableReason: 'insufficient_history',
      })] },
    });
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('regime');
    expect(text).toContain('not available');
    expect(text).toContain('history');
    // A factor that contributed nothing must not read as a neutral contribution of zero.
    expect(wrapper.text()).not.toMatch(/\braises 0\.000|lowers 0\.000/);
  });

  it('names the session each value was read as of', () => {
    const wrapper = mount(ContributionList, { props: { contributions: [contribution()] } });
    expect(wrapper.text()).toContain('2026-06-30');
  });
});
