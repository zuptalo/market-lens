import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import ChartAnnotations from './ChartAnnotations.vue';
import { buildAction, buildFinding } from '@/services/__fixtures__/marketData';

function mountAnnotations(overrides: Record<string, unknown> = {}) {
  return mount(ChartAnnotations, {
    props: {
      actions: [buildAction()],
      findings: [buildFinding()],
      missingSessions: ['2026-05-25', '2026-05-26'],
      ...overrides,
    },
  });
}

/**
 * A marker on a canvas is not a readable fact. It cannot be reached by keyboard, it says
 * nothing to a screen reader, and its label appears on hover — which the specification
 * forbids relying on. These tests hold the line that every annotation is also text.
 */
describe('ChartAnnotations', () => {
  it('states a corporate action, its date and its value without any hover', () => {
    const text = mountAnnotations().text();
    expect(text).toContain('2026-05-28');
    expect(text).toMatch(/split/i);
    expect(text).toContain('2');
  });

  it('spells out a dividend in its own currency', () => {
    const text = mountAnnotations({
      actions: [buildAction({ actionType: 'dividend', ratio: null, amount: '4.25', currency: 'SEK' })],
    }).text();
    expect(text).toMatch(/dividend/i);
    expect(text).toContain('4.25');
    expect(text).toContain('SEK');
  });

  it('names a symbol change on both sides', () => {
    const text = mountAnnotations({
      actions: [buildAction({
        actionType: 'symbol_change', ratio: null, oldSymbol: 'OLD', newSymbol: 'NEW',
      })],
    }).text();
    expect(text).toContain('OLD');
    expect(text).toContain('NEW');
  });

  it('lists an open finding with its rule, session and status', () => {
    const text = mountAnnotations().text();
    expect(text).toContain('suspicious_jump');
    expect(text).toContain('2026-05-29');
    expect(text).toContain('open');
  });

  it('says how many sessions are missing and that closed days are not among them', () => {
    const text = mountAnnotations().text();
    expect(text).toContain('2');
    // The reader must be told the difference explicitly; otherwise a short list of gaps
    // looks like a claim that the exchange only closed twice.
    expect(text).toMatch(/closed .* not gaps/i);
  });

  it('says plainly when there is nothing to warn about', () => {
    const text = mountAnnotations({ actions: [], findings: [], missingSessions: [] }).text();
    expect(text).toMatch(/no corporate actions/i);
  });

  it('puts every annotation in the document rather than behind an interaction', () => {
    const wrapper = mountAnnotations();
    // Nothing is collapsed, and nothing waits for a click: the whole point is that a reader
    // sees the discontinuity's explanation at the same time as the discontinuity.
    expect(wrapper.findAll('li').length).toBe(4);
    expect(wrapper.find('details').exists()).toBe(false);
  });
});
