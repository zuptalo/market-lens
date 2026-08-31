import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import OwnerAuth from './OwnerAuth.vue';
import { mailAccount, mailSecret } from '@/services/testSecrets';

describe('OwnerAuth clarified flow', () => {
  it('starts with only generic email and emits the same sign-in request', async () => {
    const wrapper = mount(OwnerAuth, { props: { mode: 'email' as never } });

    expect(wrapper.get('h1').text()).toBe('Sign in');
    expect(wrapper.find('input[name="password"]').exists()).toBe(false);
    expect(wrapper.find('input[name="code"]').exists()).toBe(false);
    await wrapper.get('input[name="email"]').setValue('someone@example.com');
    await wrapper.get('form').trigger('submit');

    expect(wrapper.emitted('submit')?.[0]).toEqual([{ email: 'someone@example.com' }]);
  });

  it('always presents OTP entry with an explicit secondary owner password action', async () => {
    const wrapper = mount(OwnerAuth, {
      props: {
        mode: 'otp' as never,
        email: 'someone@example.com',
        message: 'If you have an account, you should receive an email with a six-digit passcode.',
      } as never,
    });

    const code = wrapper.get('input[name="code"]');
    expect(code.attributes()).toMatchObject({ inputmode: 'numeric', autocomplete: 'one-time-code', maxlength: '6' });
    expect(wrapper.text()).toContain('Use owner password');
    expect(wrapper.text().toLowerCase()).not.toContain('forgot password');
    await code.setValue('012345');
    await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')?.[0]).toEqual([{ email: 'someone@example.com', code: '012345' }]);

    await wrapper.get('button[data-owner-password]').trigger('click');
    expect(wrapper.emitted('useOwnerPassword')).toHaveLength(1);
  });

  it('submits owner password only after the user chooses that secondary action', async () => {
    const localSet = vi.spyOn(localStorage, 'setItem');
    const sessionSet = vi.spyOn(sessionStorage, 'setItem');
    const wrapper = mount(OwnerAuth, {
      props: { mode: 'owner-password' as never, email: 'owner@example.com' } as never,
    });

    expect(wrapper.find('input[name="code"]').exists()).toBe(false);
    await wrapper.get('input[name="password"]').setValue('password-secret');
    await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')?.[0]).toEqual([{ email: 'owner@example.com', password: 'password-secret' }]);
    expect(localSet).not.toHaveBeenCalled();
    expect(sessionSet).not.toHaveBeenCalled();
  });

  it('submits complete setup without rendering or storing any setup secret', async () => {
    const localSet = vi.spyOn(localStorage, 'setItem');
    const sessionSet = vi.spyOn(sessionStorage, 'setItem');
    const wrapper = mount(OwnerAuth, {
      props: { mode: 'setup', capability: 'setup-capability-secret' },
    });

    expect(wrapper.html()).not.toContain('setup-capability-secret');
    const values: Record<string, string> = {
      displayName: 'Owner', email: 'owner@example.com', password: 'strong-password-secret',
      eodhdApiKey: 'eodhd-key-secret', smtpHost: 'smtp.example.test', smtpPort: '587',
      smtpFrom: 'access@example.test', smtpUsername: mailAccount, smtpPassword: mailSecret,
    };
    for (const [name, value] of Object.entries(values)) {
      await wrapper.get(`[name="${name}"]`).setValue(value);
    }
    await wrapper.get('form').trigger('submit');

    expect(wrapper.emitted('submit')?.[0]).toEqual([{
      capability: 'setup-capability-secret', displayName: 'Owner', email: 'owner@example.com',
      password: 'strong-password-secret', eodhdApiKey: 'eodhd-key-secret',
      smtpHost: 'smtp.example.test', smtpPort: '587', smtpFrom: 'access@example.test',
      smtpUsername: mailAccount, smtpPassword: mailSecret,
    }]);
    expect(wrapper.html()).not.toContain('eodhd-key-secret');
    expect(wrapper.html()).not.toContain('smtp-password-secret');
    expect(localSet).not.toHaveBeenCalled();
    expect(sessionSet).not.toHaveBeenCalled();
  });
});
