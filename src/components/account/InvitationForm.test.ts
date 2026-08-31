import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import InvitationForm from './InvitationForm.vue';
import type { Invitation } from '@/types/auth';

function invitation(overrides: Partial<Invitation> = {}): Invitation {
  return {
    id: '70000000-0000-4000-8000-000000000001',
    email: 'invitee@example.com',
    state: 'pending',
    expiresAt: '2026-09-06T10:00:00Z',
    acceptedAt: null,
    deliveryState: 'sent',
    deliveryError: null,
    resendCount: 0,
    createdAt: '2026-08-30T10:00:00Z',
    ...overrides,
  };
}

describe('InvitationForm', () => {
  it('invites by email alone and never asks for a password', async () => {
    const wrapper = mount(InvitationForm, { props: { invitations: [] } });
    expect(wrapper.find('input[type="password"]').exists()).toBe(false);

    const email = wrapper.get('input[name="email"]');
    expect(email.attributes('type')).toBe('email');
    expect(email.attributes('autocomplete')).toBe('email');

    await email.setValue('invitee@example.com');
    await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('invite')?.[0]).toEqual(['invitee@example.com']);
  });

  it('reports an empty roster rather than an empty table', () => {
    const wrapper = mount(InvitationForm, { props: { invitations: [] } });
    expect(wrapper.text()).toContain('No invitations yet');
  });

  it('shows safe delivery state and offers resend and revoke only while pending', async () => {
    const wrapper = mount(InvitationForm, {
      props: {
        invitations: [
          invitation({ id: 'a', deliveryState: 'sent' }),
          invitation({ id: 'b', email: 'failed@example.com', deliveryState: 'failed', deliveryError: 'temporary_failure' }),
          invitation({ id: 'c', email: 'done@example.com', state: 'accepted', acceptedAt: '2026-08-31T10:00:00Z' }),
        ],
      },
    });

    const text = wrapper.text();
    expect(text).toContain('Sent');
    expect(text).toContain('Not delivered');
    expect(text).toContain('Accepted');
    // A provider failure must be actionable without exposing provider internals.
    expect(text).not.toContain('temporary_failure');
    expect(text).not.toContain('smtp');

    // Accepted invitations are historical; only pending ones can be resent or revoked.
    expect(wrapper.findAll('button[data-resend]')).toHaveLength(2);
    expect(wrapper.findAll('button[data-revoke]')).toHaveLength(2);

    await wrapper.findAll('button[data-resend]')[1].trigger('click');
    expect(wrapper.emitted('resend')?.[0]).toEqual(['b']);
    await wrapper.findAll('button[data-revoke]')[0].trigger('click');
    expect(wrapper.emitted('revoke')?.[0]).toEqual(['a']);
  });

  it('labels each action with the address it affects', () => {
    const wrapper = mount(InvitationForm, { props: { invitations: [invitation()] } });
    expect(wrapper.get('button[data-resend]').attributes('aria-label')).toContain('invitee@example.com');
    expect(wrapper.get('button[data-revoke]').attributes('aria-label')).toContain('invitee@example.com');
  });

  it('disables every control while a request is in flight', () => {
    const wrapper = mount(InvitationForm, { props: { invitations: [invitation()], busy: true } });
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined();
    expect(wrapper.get('button[data-resend]').attributes('disabled')).toBeDefined();
    expect(wrapper.get('button[data-revoke]').attributes('disabled')).toBeDefined();
  });

  it('surfaces safe errors and confirmations', () => {
    const failed = mount(InvitationForm, {
      props: { invitations: [], error: 'That address already has access or a pending invitation.' },
    });
    expect(failed.get('[role="alert"]').text()).toContain('already has access');

    const sent = mount(InvitationForm, { props: { invitations: [], message: 'Invitation sent.' } });
    expect(sent.get('[role="status"]').text()).toBe('Invitation sent.');
  });
});
