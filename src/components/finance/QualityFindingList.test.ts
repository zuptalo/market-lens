import { mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { describe, expect, it } from 'vitest';
import QualityFindingList from './QualityFindingList.vue';
import type { QualityFinding } from '@/types/marketData';

function finding(overrides: Partial<QualityFinding> = {}): QualityFinding {
  return {
    id: 'f1',
    rule: 'provider_gap',
    status: 'open',
    sessionDate: '2017-04-13',
    detail: 'Provider returned a bar outside the expected exchange sessions.',
    instrumentId: '22000000-0000-4000-8000-000000000001',
    ticker: 'EQNR',
    severity: 'warning',
    reexaminedAt: '2026-09-08T18:00:00Z',
    awaitingDecision: true,
    ...overrides,
  };
}

function mountList(findings: QualityFinding[], props: Record<string, unknown> = {}) {
  return mount(QualityFindingList, {
    props: { findings, ...props }, global: { plugins: [PrimeVue] },
  });
}

describe('QualityFindingList', () => {
  it('says what was observed, not just which rule fired', () => {
    const wrapper = mountList([finding()]);
    const text = wrapper.text();
    expect(text).toContain('EQNR');
    expect(text).toContain('2017-04-13');
    // The rule name is the product's vocabulary; somebody judging the condition needs the
    // observation in their own.
    expect(text).toContain('session the exchange calendar does not have');
    expect(text).toContain('provider_gap');
  });

  // The decision is the operator's, and the product must say so rather than implying it has
  // already concluded anything.
  it('states that accepting is the reader’s judgement', () => {
    const wrapper = mountList([finding()]);
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('did not change');
    expect(text).toContain('the product does not decide that for you');
  });

  it('names the finding its action acts on', () => {
    const wrapper = mountList([finding()]);
    const button = wrapper.findAll('button').find((b) => b.text().includes('Accept'));
    expect(button?.attributes('aria-label')).toContain('provider_gap');
    expect(button?.attributes('aria-label')).toContain('2017-04-13');
  });

  it('emits the finding to accept rather than deciding locally', async () => {
    const wrapper = mountList([finding()]);
    const button = wrapper.findAll('button').find((b) => b.text().includes('Accept'));
    await button?.trigger('click');
    expect(wrapper.emitted('accept')?.[0]).toEqual(['f1']);
  });

  it('says nothing is waiting rather than showing an empty table', () => {
    const wrapper = mountList([]);
    expect(wrapper.text().toLowerCase()).toContain('nothing is waiting for you');
    expect(wrapper.find('[data-testid="quality-finding-list"]').exists()).toBe(false);
  });

  it('shows a first load where the findings will be', () => {
    const wrapper = mountList([], { loading: true });
    expect(wrapper.find('[data-testid="loading-block"]').exists()).toBe(true);
    expect(wrapper.text().toLowerCase()).not.toContain('nothing is waiting');
  });
});
