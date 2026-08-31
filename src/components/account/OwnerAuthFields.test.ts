import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import OwnerAuth from '@/components/account/OwnerAuth.vue';

// Feature 010. Setup used to answer "The request is invalid." whatever was wrong, leaving the
// person filling in ten inputs to guess which one to change.
describe('OwnerAuth setup field errors', () => {
  const fields = {
    password: 'Password must be at least 12 characters.',
    smtp_port: 'SMTP port must be between 1 and 65535. It is usually 587.',
  };

  it('shows each message against its own input and marks that input invalid', () => {
    const wrapper = mount(OwnerAuth, {
      props: { mode: 'setup', capability: 'c', fieldErrors: fields },
    });

    const password = wrapper.get('#owner-password');
    expect(password.attributes('aria-invalid')).toBe('true');
    const describedBy = password.attributes('aria-describedby');
    expect(describedBy).toBeTruthy();
    expect(wrapper.get(`#${describedBy}`).text()).toContain('at least 12 characters');

    const port = wrapper.get('#smtp-port');
    expect(port.attributes('aria-invalid')).toBe('true');
    expect(wrapper.get(`#${port.attributes('describedby') ?? port.attributes('aria-describedby')}`).text())
      .toContain('between 1 and 65535');
  });

  it('leaves inputs without an error untouched', () => {
    const wrapper = mount(OwnerAuth, {
      props: { mode: 'setup', capability: 'c', fieldErrors: fields },
    });
    const email = wrapper.get('#owner-email');
    expect(email.attributes('aria-invalid')).toBeUndefined();
    expect(email.attributes('aria-describedby')).toBeUndefined();
  });

  it('summarises how many inputs need attention so the count is not hidden below the fold', () => {
    const wrapper = mount(OwnerAuth, {
      props: { mode: 'setup', capability: 'c', error: 'Some of the details you entered need attention.', fieldErrors: fields },
    });
    const alert = wrapper.get('[role="alert"]');
    expect(alert.text()).toContain('need attention');
    expect(alert.text()).toContain('2');
  });

  it('renders no field errors when there are none', () => {
    const wrapper = mount(OwnerAuth, { props: { mode: 'setup', capability: 'c' } });
    expect(wrapper.find('.owner-auth__field-error').exists()).toBe(false);
    expect(wrapper.get('#owner-password').attributes('aria-invalid')).toBeUndefined();
  });
});
