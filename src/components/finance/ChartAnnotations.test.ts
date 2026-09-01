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
    // The rule is shown as a phrase rather than as its identifier; the three facts FR-016
    // asks for are all still present.
    expect(text).toContain('Suspicious jump');
    expect(text).toContain('2026-05-29');
    expect(text).toContain('open');
  });

  it('says how many sessions are missing and that closed days are not among them', () => {
    const text = mountAnnotations().text();
    expect(text).toContain('2');
    // The reader must be told the difference explicitly; otherwise a short list of gaps
    // looks like a claim that the exchange only closed twice.
    expect(text).toMatch(/exchange was closed is not listed here and is not a gap/i);
  });

  it('says plainly when there is nothing to warn about', () => {
    const text = mountAnnotations({ actions: [], findings: [], missingSessions: [] }).text();
    expect(text).toMatch(/no corporate actions/i);
  });

  it('states a repeated explanation once rather than on every row', () => {
    const wrapper = mountAnnotations({
      findings: [
        buildFinding({ id: 'f1', sessionDate: '2021-09-02', rule: 'missing_session' }),
        buildFinding({ id: 'f2', sessionDate: '2021-09-03', rule: 'missing_session' }),
        buildFinding({ id: 'f3', sessionDate: '2021-09-06', rule: 'missing_session' }),
      ],
      actions: [],
      missingSessions: [],
    });
    const text = wrapper.text();
    const explanation = 'close moved more than the configured threshold';
    const occurrences = text.split(explanation).length - 1;
    // Eighty-seven findings each repeating the same sentence is a wall nobody reads. The
    // explanation belongs to the rule, so it is stated once and the affected sessions listed
    // beneath it.
    expect(occurrences).toBeLessThanOrEqual(1);
    for (const session of ['2021-09-02', '2021-09-03', '2021-09-06']) {
      expect(text, `session ${session} is no longer listed`).toContain(session);
    }
  });

  it('counts the sessions a rule affects so the scale is legible at a glance', () => {
    const wrapper = mountAnnotations({
      findings: [
        buildFinding({ id: 'f1', sessionDate: '2021-09-02' }),
        buildFinding({ id: 'f2', sessionDate: '2021-09-03' }),
      ],
      actions: [],
      missingSessions: [],
    });
    expect(wrapper.text()).toMatch(/2 sessions/);
  });

  it('names a rule in words rather than in its identifier', () => {
    const wrapper = mountAnnotations({
      findings: [buildFinding({ rule: 'missing_session' })],
      actions: [],
      missingSessions: [],
    });
    const text = wrapper.text();
    expect(text).toContain('Missing session');
    expect(text).not.toContain('missing_session');
    expect(text).not.toContain('Missing_session');
  });

  it('keeps rules apart so two different problems are not merged', () => {
    const wrapper = mountAnnotations({
      findings: [
        buildFinding({ id: 'a', rule: 'missing_session', sessionDate: '2021-09-02' }),
        buildFinding({ id: 'b', rule: 'suspicious_jump', sessionDate: '2021-09-03' }),
      ],
      actions: [],
      missingSessions: [],
    });
    const text = wrapper.text();
    expect(text).toContain('Missing session');
    expect(text).toContain('Suspicious jump');
  });

  it('puts every annotation in the document rather than behind an interaction', () => {
    const wrapper = mountAnnotations();
    const text = wrapper.text();
    // Nothing is collapsed, and nothing waits for a click: the whole point is that a reader
    // sees the discontinuity's explanation at the same time as the discontinuity. Asserted on
    // what is readable rather than on a row count, which is a presentation choice.
    expect(wrapper.find('details').exists()).toBe(false);
    for (const fact of ['2026-05-28', 'Split', '2026-05-29', 'Suspicious jump', '2026-05-25', '2026-05-26']) {
      expect(text, `${fact} is not readable`).toContain(fact);
    }
  });
});
