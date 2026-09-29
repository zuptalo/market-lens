import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import FallbackBanner from './FallbackBanner.vue';

function banner(state: { active: boolean; since: string | null; instruments: number; pendingReconciliation: boolean } | null) {
  return mount(FallbackBanner, { props: { state }, global: { plugins: [PrimeVue] } });
}

describe('FallbackBanner', () => {
  it('says nothing on an ordinary day, or before the state is known', () => {
    expect(banner(null).text()).toBe('');
    expect(banner({ active: false, since: null, instruments: 0, pendingReconciliation: false }).text()).toBe('');
  });

  /**
   * The whole point of the banner: a fallback nobody can see hides the problem it is covering for.
   * It says since when, how many, and what is held back while it lasts.
   */
  it('states the source, since when, how many, and what waits', () => {
    const wrapper = banner({ active: true, since: '2026-09-29', instruments: 100, pendingReconciliation: false });
    const text = wrapper.text();
    expect(text).toContain('fallback');
    expect(text).toContain('100 instruments');
    expect(text).toMatch(/29.*(Sep|sep|09)|2026-09-29/);
    expect(text.toLowerCase()).toContain('paper orders wait');
    expect(text.toLowerCase()).toContain('backtests');
    // Announced to assistive technology, and never dependent on colour alone.
    expect(wrapper.find('[role="status"]').exists()).toBe(true);
  });

  it('says one instrument, not one instruments', () => {
    const text = banner({ active: true, since: '2026-09-29', instruments: 1, pendingReconciliation: false }).text();
    expect(text).toContain('1 instrument ');
  });

  it('says when the primary is back but older fallback prices remain', () => {
    const text = banner({ active: true, since: '2026-09-29', instruments: 12, pendingReconciliation: true }).text();
    expect(text.toLowerCase()).toContain('primary provider is delivering again');
    expect(text.toLowerCase()).toContain('reconcil');
  });

  it('tells nobody what to do with their money', () => {
    const text = banner({ active: true, since: '2026-09-29', instruments: 100, pendingReconciliation: false })
      .text().toLowerCase();
    for (const forbidden of ['recommend', 'suggest', 'you should', 'buy', 'sell']) {
      expect(text).not.toContain(forbidden);
    }
  });
});
