import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import IntentList from './IntentList.vue';
import type { OrderIntent } from '@/types/marketData';

function intent(overrides: Partial<OrderIntent> = {}): OrderIntent {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    instrumentId: '33333333-3333-4333-8333-333333333333',
    ticker: 'VOLV-B',
    name: 'Volvo B',
    currency: 'SEK',
    direction: 'buy',
    quantity: '120.000000000000',
    price: '281.400000000000',
    costs: '0.000000000000',
    status: 'considering',
    recordedAt: '2026-09-18T09:00:00Z',
    settledAt: null,
    consequence: {
      resultingQuantity: '520.000000000000',
      resultingValue: '146328.000000000000',
      resultingShare: '0.412300000000',
      denominator: '354900.000000000000',
      absenceReason: null,
      limits: [{
        kind: 'instrument_share', threshold: '0.250000000000', state: 'exceeded',
        measured: '0.412300000000', denominator: '354900.000000000000',
        absenceReason: null, contributions: [],
      }],
    },
    ...overrides,
  };
}

describe('IntentList', () => {
  it('says what the position would become, and what it would be worth', () => {
    const wrapper = mount(IntentList, { props: { intents: [intent()], currency: 'SEK' } });
    const text = wrapper.text();
    expect(text).toContain('VOLV-B');
    expect(text).toContain('520');
    expect(text).toContain('146,328');
    expect(text).toContain('41.2%');
    // The total the share was measured against, so the percentage can be checked.
    expect(text).toContain('354,900');
  });

  // The same rule as the limits screen: a person who cannot tell red from green must not have to
  // guess whether acting would put them outside their own rule.
  it('says a limit verdict in words rather than only in colour', () => {
    const wrapper = mount(IntentList, { props: { intents: [intent()], currency: 'SEK' } });
    expect(wrapper.text()).toContain('Over');
    expect(wrapper.text()).toContain('Most in any one company');
  });

  it('never tells the person what to do', () => {
    const wrapper = mount(IntentList, {
      props: {
        currency: 'SEK',
        intents: [
          intent(),
          intent({
            id: 'b', direction: 'sell', status: 'considering',
            consequence: {
              resultingQuantity: '-50.000000000000', resultingValue: null,
              resultingShare: null, denominator: null,
              absenceReason: 'portfolio_incomplete', limits: [],
            },
          }),
        ],
      },
    });
    const text = wrapper.text().toLowerCase();
    for (const forbidden of ['recommend', 'suggest', 'you should', 'advice', 'signal says',
      'place order', 'submit order', 'broker']) {
      expect(text).not.toContain(forbidden);
    }
  });

  it('states why a consequence could not be computed rather than showing a zero', () => {
    const wrapper = mount(IntentList, {
      props: {
        currency: 'SEK',
        intents: [intent({
          consequence: {
            resultingQuantity: '520.000000000000', resultingValue: null, resultingShare: null,
            denominator: null, absenceReason: 'portfolio_incomplete', limits: [],
          },
        })],
      },
    });
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('could not be priced');
    expect(text).toContain('not measured');
    expect(text).not.toContain('0.0%');
  });

  // An intent proposing to sell more than is held is reported, not hidden.
  it('shows a negative resulting position with a word, not only a minus sign', () => {
    const wrapper = mount(IntentList, {
      props: {
        currency: 'SEK',
        intents: [intent({
          direction: 'sell',
          consequence: {
            resultingQuantity: '-50.000000000000', resultingValue: null, resultingShare: null,
            denominator: null, absenceReason: null, limits: [],
          },
        })],
      },
    });
    expect(wrapper.text().toLowerCase()).toContain('more than you hold');
  });

  it('offers withdraw and acted-on only while an intent is still being considered', async () => {
    const wrapper = mount(IntentList, {
      props: {
        currency: 'SEK',
        intents: [intent(), intent({
          id: 'settled', status: 'acted_on', settledAt: '2026-09-18T10:00:00Z',
          consequence: null,
        })],
      },
    });
    const buttons = wrapper.findAll('button').map((button) => button.text().toLowerCase());
    expect(buttons.filter((label) => label.includes('withdraw'))).toHaveLength(1);
    expect(buttons.filter((label) => label.includes('acted on this'))).toHaveLength(1);
    expect(wrapper.text()).toContain('Acted on');
    // A settled intent is shown without a consequence rather than with a zeroed one.
    expect(wrapper.text()).toContain('no longer evaluated');
  });

  it('emits the settlement the person chose', async () => {
    const wrapper = mount(IntentList, { props: { intents: [intent()], currency: 'SEK' } });
    const withdraw = wrapper.findAll('button').find((b) => b.text().toLowerCase().includes('withdraw'));
    await withdraw?.trigger('click');
    expect(wrapper.emitted('settle')?.[0]).toEqual([
      '11111111-1111-4111-8111-111111111111', 'withdrawn',
    ]);
  });

  it('says when there is nothing to show rather than showing an empty table', () => {
    const wrapper = mount(IntentList, { props: { intents: [], currency: 'SEK' } });
    expect(wrapper.text().toLowerCase()).toContain('not considering anything');
  });
});
