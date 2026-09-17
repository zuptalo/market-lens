import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import IntentForm from './IntentForm.vue';
const instruments = [
  { id: '33333333-3333-4333-8333-333333333333', ticker: 'VOLV-B', name: 'Volvo B' },
  { id: '44444444-4444-4444-8444-444444444444', ticker: 'NOKIA', name: 'Nokia' },
];

function mountForm(props: Record<string, unknown> = {}) {
  return mount(IntentForm, { props: { instruments, ...props } });
}

describe('IntentForm', () => {
  it('offers no default quantity or price that could be read as a recommendation', () => {
    const wrapper = mountForm();
    const inputs = wrapper.findAll('input');
    for (const input of inputs) {
      expect((input.element as HTMLInputElement).value).toBe('');
    }
  });

  it('will not submit until the person has said what they are considering', async () => {
    const wrapper = mountForm();
    const submit = wrapper.find('button[type="submit"]');
    expect(submit.attributes('disabled')).toBeDefined();
    await wrapper.findComponent({ name: 'Select' }).setValue(instruments[0].id);
    expect(wrapper.find('button[type="submit"]').attributes('disabled')).toBeDefined();
  });

  it('emits what the person entered, with no field a broker could act on', async () => {
    const wrapper = mountForm();
    const selects = wrapper.findAllComponents({ name: 'Select' });
    await selects[0].setValue(instruments[0].id);
    await selects[1].setValue('sell');
    const numbers = wrapper.findAllComponents({ name: 'InputNumber' });
    await numbers[0].setValue(120);
    await numbers[1].setValue(281.4);
    await wrapper.find('form').trigger('submit');

    const emitted = wrapper.emitted('submit');
    expect(emitted).toBeTruthy();
    const input = emitted?.[0]?.[0] as Record<string, unknown>;
    expect(input).toEqual({
      instrumentId: instruments[0].id,
      direction: 'sell',
      quantity: '120',
      price: '281.4',
      costs: '0',
    });
    for (const forbidden of ['venue', 'orderType', 'timeInForce', 'destination', 'expiresAt']) {
      expect(input).not.toHaveProperty(forbidden);
    }
  });

  it('says plainly that writing one down does nothing', () => {
    const text = mountForm().text().toLowerCase();
    expect(text).toContain('nothing is sent anywhere');
    // It must say the product places none, and must never offer to place one.
    expect(text).toContain('places no orders');
    for (const forbidden of ['recommend', 'suggest', 'you should', 'place an order',
      'place order', 'submit order', 'send order']) {
      expect(text).not.toContain(forbidden);
    }
  });

  it('shows a refusal the product gave rather than swallowing it', () => {
    const wrapper = mountForm({ refusal: 'This product does not carry that instrument.' });
    expect(wrapper.text()).toContain('This product does not carry that instrument.');
  });

  it('names every field for a screen reader, not only by its current value', () => {
    const wrapper = mountForm();
    for (const select of wrapper.findAllComponents({ name: 'Select' })) {
      expect(select.props('ariaLabel') ?? select.attributes('aria-label')).toBeTruthy();
    }
    for (const number of wrapper.findAllComponents({ name: 'InputNumber' })) {
      expect(number.props('inputId')).toBeTruthy();
    }
  });
});
