import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import SignalCard from './SignalCard.vue';
import type { Signal } from '@/types/marketData';

const strategy = {
  name: 'momentum_trend', version: 1, title: 'Momentum and trend',
  caveat: 'Its weights are stated rather than fitted; nothing it produces is a prediction.',
  superseded: false,
};

function scored(overrides: Partial<Signal> = {}): Signal {
  return {
    instrumentId: '44000000-0000-4000-8000-000000000001',
    sessionDate: '2026-06-30',
    strategy,
    score: '0.412500000000',
    action: 'WATCH',
    confidence: '0.875000000000',
    absenceReason: null,
    divisor: '1.000000000000',
    computedAt: '2026-07-01T04:00:00Z',
    contributions: [],
    ...overrides,
  };
}

describe('SignalCard', () => {
  it('shows the action, the score, the confidence and the caveat', () => {
    const wrapper = mount(SignalCard, { props: { signal: scored() } });
    const text = wrapper.text();
    expect(text).toContain('WATCH');
    expect(text).toContain('0.41');
    expect(text).toContain('momentum_trend');
    expect(text).toContain('2026-06-30');
    expect(text).toContain(strategy.caveat);
  });

  // SC-006. The statement is on the card itself, not on the page around it, so it travels
  // wherever the card is used.
  it('states that a signal is not advice', () => {
    const wrapper = mount(SignalCard, { props: { signal: scored() } });
    expect(wrapper.text().toLowerCase()).toContain('not advice');
  });

  // FR-013a. Confidence measures agreement between factors; calling it a probability would be
  // the single most misleading label on the screen.
  it('describes confidence as factor agreement rather than as a probability', () => {
    const wrapper = mount(SignalCard, { props: { signal: scored() } });
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('agreement');
    expect(text).not.toContain('probability');
    expect(text).not.toContain('likelihood');
  });

  it('renders no action at all for an absent signal, in particular never HOLD', () => {
    const wrapper = mount(SignalCard, {
      props: { signal: scored({
        score: null, action: null, confidence: null, divisor: null,
        absenceReason: 'insufficient_history',
      }) },
    });
    const text = wrapper.text();
    expect(text).not.toContain('HOLD');
    expect(text).not.toContain('WATCH');
    expect(text.toLowerCase()).toContain('no view');
    expect(text.toLowerCase()).toContain('history');
    expect(text).toContain(strategy.caveat);
  });

  it('says so when a superseded version produced the signal', () => {
    const wrapper = mount(SignalCard, {
      props: { signal: scored({ strategy: { ...strategy, superseded: true, version: 1 } }) },
    });
    expect(wrapper.text().toLowerCase()).toContain('superseded');
  });

  it('says plainly when no strategy has recorded a view', () => {
    const wrapper = mount(SignalCard, { props: { signal: null } });
    expect(wrapper.text().toLowerCase()).toContain('no strategy has recorded');
  });
});
