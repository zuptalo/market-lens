import { describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import AcceptInvitationView from './AcceptInvitationView.vue';

const push = vi.fn();
vi.mock('vue-router', () => ({
  useRouter: () => ({ replace: push }),
  useRoute: () => ({ query: {} }),
}));

const authDouble: { acceptInvitation: ReturnType<typeof vi.fn> } = { acceptInvitation: vi.fn() };
vi.mock('@/composables/useAuth', () => ({ useAuth: () => authDouble }));

function mountView(auth: { acceptInvitation: ReturnType<typeof vi.fn> }, hash = '#invitation-capability-secret') {
  authDouble.acceptInvitation = auth.acceptInvitation;
  window.location.hash = hash;
  return mount(AcceptInvitationView);
}

describe('AcceptInvitationView', () => {
  it('accepts an invitation with an email and name and never asks for a password', async () => {
    const acceptInvitation = vi.fn().mockResolvedValue(undefined);
    const wrapper = mountView({ acceptInvitation });

    expect(wrapper.find('input[type="password"]').exists()).toBe(false);
    expect(wrapper.text().toLowerCase()).not.toContain('password');

    await wrapper.get('input[name="email"]').setValue('invitee@example.com');
    await wrapper.get('input[name="displayName"]').setValue('Ada Invitee');
    await wrapper.get('form').trigger('submit');
    await Promise.resolve();

    expect(acceptInvitation).toHaveBeenCalledWith({
      capability: 'invitation-capability-secret',
      email: 'invitee@example.com',
      displayName: 'Ada Invitee',
    });
  });

  it('reports a missing capability without contacting the server', async () => {
    const acceptInvitation = vi.fn();
    const wrapper = mountView({ acceptInvitation }, '');

    await wrapper.get('input[name="email"]').setValue('invitee@example.com');
    await wrapper.get('input[name="displayName"]').setValue('Ada Invitee');
    await wrapper.get('form').trigger('submit');
    await Promise.resolve();

    expect(acceptInvitation).not.toHaveBeenCalled();
    expect(wrapper.get('[role="alert"]').text()).toContain('invalid or unavailable');
  });

  it('keeps the capability out of the address bar once it has been read', () => {
    mountView({ acceptInvitation: vi.fn() });
    // Leaving the capability in the URL would put it in history and any copied link.
    expect(window.location.hash).toBe('');
  });

  it('shows a generic failure and retains typed input', async () => {
    const acceptInvitation = vi.fn().mockRejectedValue(new Error('The request is invalid or unavailable.'));
    const wrapper = mountView({ acceptInvitation });

    await wrapper.get('input[name="email"]').setValue('invitee@example.com');
    await wrapper.get('input[name="displayName"]').setValue('Ada Invitee');
    await wrapper.get('form').trigger('submit');
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(wrapper.get('[role="alert"]').text()).toContain('invalid or unavailable');
    expect((wrapper.get('input[name="email"]').element as HTMLInputElement).value).toBe('invitee@example.com');
    expect((wrapper.get('input[name="displayName"]').element as HTMLInputElement).value).toBe('Ada Invitee');
  });
});
