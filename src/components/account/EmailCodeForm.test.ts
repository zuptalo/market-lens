import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import EmailCodeForm from './EmailCodeForm.vue';

describe('EmailCodeForm', () => {
  it('offers a mobile-friendly one-time-code field that accepts only six digits', async () => {
    const wrapper = mount(EmailCodeForm, { props: { email: 'member@example.com' } });
    const input = wrapper.get('input[name="code"]');

    // Mobile keyboards must show digits and the OS must be able to autofill the SMS/email code.
    expect(input.attributes('inputmode')).toBe('numeric');
    expect(input.attributes('autocomplete')).toBe('one-time-code');
    expect(input.attributes('pattern')).toBe('[0-9]{6}');
    expect(input.attributes('type')).not.toBe('password');
    // The raw field must hold more than six characters so a formatted paste such as
    // "01 23-45" is not truncated before its separators are stripped.
    expect(Number(input.attributes('maxlength'))).toBeGreaterThan(8);

    // The address being verified is shown so the person knows where the code went.
    expect(wrapper.text()).toContain('member@example.com');
  });

  it('normalises pasted codes and submits the trimmed digits', async () => {
    const wrapper = mount(EmailCodeForm, { props: { email: 'member@example.com' } });
    const input = wrapper.get('input[name="code"]');

    await input.setValue(' 01 23-45 ');
    expect((input.element as HTMLInputElement).value).toBe('012345');

    // Over-typing is capped at six digits rather than silently accepted.
    await input.setValue('01234567');
    expect((input.element as HTMLInputElement).value).toBe('012345');
    await input.setValue(' 01 23-45 ');

    await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')?.[0]).toEqual([{ email: 'member@example.com', code: '012345' }]);
  });

  it('refuses to submit an incomplete code without contacting the server', async () => {
    const wrapper = mount(EmailCodeForm, { props: { email: 'member@example.com' } });
    await wrapper.get('input[name="code"]').setValue('123');
    await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')).toBeUndefined();
    expect(wrapper.text()).toContain('six digits');
  });

  it('shows a resend countdown and only enables resend when it reaches zero', async () => {
    const wrapper = mount(EmailCodeForm, { props: { email: 'member@example.com', resendIn: 45 } });
    const resend = wrapper.get('button[data-resend]');
    expect(resend.attributes('disabled')).toBeDefined();
    expect(wrapper.text()).toContain('45');

    await wrapper.setProps({ resendIn: 0 });
    expect(wrapper.get('button[data-resend]').attributes('disabled')).toBeUndefined();
    await wrapper.get('button[data-resend]').trigger('click');
    expect(wrapper.emitted('resend')).toHaveLength(1);
  });

  it('surfaces generic errors and keeps the typed code so an outage does not lose input', async () => {
    const wrapper = mount(EmailCodeForm, {
      props: { email: 'member@example.com', error: 'Authentication failed.' },
    });
    await wrapper.get('input[name="code"]').setValue('012345');
    await wrapper.setProps({ error: 'Authentication failed.', busy: false });

    const alert = wrapper.get('[role="alert"]');
    expect(alert.text()).toBe('Authentication failed.');
    // A generic failure must never hint at whether the account exists or is locked.
    expect(wrapper.text().toLowerCase()).not.toContain('locked');
    expect(wrapper.text().toLowerCase()).not.toContain('blocked');
    expect(wrapper.text().toLowerCase()).not.toContain('no such');
    expect((wrapper.get('input[name="code"]').element as HTMLInputElement).value).toBe('012345');
  });

  it('disables submission while a verification is in flight', async () => {
    const wrapper = mount(EmailCodeForm, { props: { email: 'member@example.com', busy: true } });
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined();
  });
});
