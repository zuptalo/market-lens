import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import IntegrationSettings from '@/components/account/IntegrationSettings.vue';
import type { IntegrationSettingsView } from '@/types/auth';

const settings: IntegrationSettingsView = {
  eodhd: { configured: true, validatedAt: '2026-08-31T09:00:00Z' },
  smtp: {
    configured: true, host: 'smtp.example.test', port: 587,
    from: 'access@example.test', username: 'mailer', passwordConfigured: true,
  },
};

describe('IntegrationSettings', () => {
  it('shows what the installation is configured to use', () => {
    const wrapper = mount(IntegrationSettings, { props: { settings, busy: false } });
    expect((wrapper.get('#integration-smtp-host').element as HTMLInputElement).value).toBe('smtp.example.test');
    expect((wrapper.get('#integration-smtp-port').element as HTMLInputElement).value).toBe('587');
    expect((wrapper.get('#integration-smtp-username').element as HTMLInputElement).value).toBe('mailer');
    expect(wrapper.text()).toContain('A password is saved');
  });

  it('never prefills a secret, because a secret is never returned', () => {
    const wrapper = mount(IntegrationSettings, { props: { settings, busy: false } });
    expect((wrapper.get('#integration-smtp-password').element as HTMLInputElement).value).toBe('');
    expect((wrapper.get('#integration-eodhd-api-key').element as HTMLInputElement).value).toBe('');
  });

  it('omits an untouched password so the stored one is kept', async () => {
    const wrapper = mount(IntegrationSettings, { props: { settings, busy: false } });
    await wrapper.get('#integration-smtp-port').setValue('2525');
    await wrapper.get('form').trigger('submit');

    const [submitted] = wrapper.emitted('save')?.[0] as [Record<string, unknown>];
    expect((submitted.smtp as Record<string, unknown>).port).toBe(2525);
    expect((submitted.smtp as Record<string, unknown>).password).toBeUndefined();
    // An untouched provider key must not be sent as an empty replacement.
    expect(submitted.eodhd).toBeUndefined();
  });

  it('sends a typed password and provider key when they are changed', async () => {
    const wrapper = mount(IntegrationSettings, { props: { settings, busy: false } });
    await wrapper.get('#integration-smtp-password').setValue('a-new-password');
    await wrapper.get('#integration-eodhd-api-key').setValue('a-new-key');
    await wrapper.get('form').trigger('submit');

    const [submitted] = wrapper.emitted('save')?.[0] as [Record<string, unknown>];
    expect((submitted.smtp as Record<string, unknown>).password).toBe('a-new-password');
    expect((submitted.eodhd as Record<string, unknown>).apiKey).toBe('a-new-key');
  });

  it('offers a check that does not save', async () => {
    const wrapper = mount(IntegrationSettings, { props: { settings, busy: false } });
    await wrapper.get('[data-verify]').trigger('click');
    expect(wrapper.emitted('verify')).toHaveLength(1);
    expect(wrapper.emitted('save')).toBeUndefined();
  });

  it('shows each field error against its own input', () => {
    const wrapper = mount(IntegrationSettings, {
      props: {
        settings, busy: false,
        error: 'Some of the details you entered need attention.',
        fieldErrors: { smtp_password: 'The mail server rejected these credentials.' },
      },
    });
    const password = wrapper.get('#integration-smtp-password');
    expect(password.attributes('aria-invalid')).toBe('true');
    const describedBy = password.attributes('aria-describedby');
    expect(wrapper.get(`#${describedBy}`).text()).toContain('rejected these credentials');
  });

  it('disables both actions while a request is in flight', () => {
    const wrapper = mount(IntegrationSettings, { props: { settings, busy: true } });
    expect(wrapper.get('[data-verify]').attributes('disabled')).toBeDefined();
    expect(wrapper.get('[type="submit"]').attributes('disabled')).toBeDefined();
  });
});

describe('IntegrationSettings per-section status', () => {
  const base = { settings, busy: false } as const;

  it('confirms each section separately when both verify', () => {
    const wrapper = mount(IntegrationSettings, {
      props: { ...base, results: { eodhd: 'verified', smtp: 'verified' } },
    });
    expect(wrapper.get('[data-status="eodhd"]').text()).toContain('EODHD accepted this API key');
    expect(wrapper.get('[data-status="smtp"]').text()).toContain('mail server accepted');
  });

  it('shows one section green and the other red', () => {
    const wrapper = mount(IntegrationSettings, {
      props: {
        ...base,
        results: { eodhd: 'verified', smtp: 'failed' },
        fieldErrors: { smtp_password: 'The mail server rejected these credentials.' },
      },
    });
    expect(wrapper.get('[data-status="eodhd"]').attributes('data-severity')).toBe('success');
    expect(wrapper.get('[data-status="smtp"]').attributes('data-severity')).toBe('error');
  });

  // The honest case: a bad port stops every network call, so neither integration was contacted
  // and neither may be reported as working.
  it('says nothing was checked rather than claiming success', async () => {
    const wrapper = mount(IntegrationSettings, { props: base });
    // A key was entered, so both integrations were submitted and a bad port stopped both.
    await wrapper.get('#integration-eodhd-api-key').setValue('a-new-key');
    await wrapper.get('[data-verify]').trigger('click');
    await wrapper.setProps({
      results: { eodhd: 'not_checked', smtp: 'not_checked' },
      fieldErrors: { smtp_port: 'SMTP port must be between 1 and 65535.' },
    });
    expect(wrapper.get('[data-status="eodhd"]').text()).toContain('not checked');
    expect(wrapper.get('[data-status="eodhd"]').attributes('data-severity')).toBe('warn');
    expect(wrapper.get('[data-status="smtp"]').text()).toContain('not checked');
  });

  it('shows no section status before anything has been checked', () => {
    const wrapper = mount(IntegrationSettings, { props: base });
    expect(wrapper.find('[data-status="eodhd"]').exists()).toBe(false);
    expect(wrapper.find('[data-status="smtp"]').exists()).toBe(false);
  });
});

describe('IntegrationSettings unchecked reasons', () => {
  it('distinguishes a section left blank from one the server skipped', async () => {
    const wrapper = mount(IntegrationSettings, { props: { settings, busy: false } });
    // Nothing typed into the provider key, so it was never submitted.
    await wrapper.get('[data-verify]').trigger('click');
    await wrapper.setProps({ results: { eodhd: 'not_checked', smtp: 'verified' } });
    expect(wrapper.get('[data-status="eodhd"]').text()).toContain('saved key is unchanged');
    expect(wrapper.get('[data-status="eodhd"]').attributes('data-severity')).toBe('info');

    // A key was typed, so "not checked" really does mean something else blocked it.
    await wrapper.get('#integration-eodhd-api-key').setValue('a-new-key');
    await wrapper.get('[data-verify]').trigger('click');
    await wrapper.setProps({ results: { eodhd: 'not_checked', smtp: 'not_checked' } });
    expect(wrapper.get('[data-status="eodhd"]').text()).toContain('needs fixing first');
    expect(wrapper.get('[data-status="eodhd"]').attributes('data-severity')).toBe('warn');
  });
});
