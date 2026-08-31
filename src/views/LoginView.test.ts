import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import LoginView from './LoginView.vue';

const push = vi.fn();
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ replace: push, push }),
}));

const authState = {
  status: 'anonymous' as const, account: null, csrfToken: null, connection: 'offline' as const,
  error: null, signInStep: 'email' as const, signInEmail: '', signInMessage: null,
};
const setupStatus = vi.fn();
vi.mock('@/composables/useAuth', () => ({
  useAuth: () => ({
    state: authState,
    setupStatus: (...args: unknown[]) => setupStatus(...args),
    startSignIn: vi.fn(), loginOwner: vi.fn(), loginMemberCode: vi.fn(),
    selectOwnerPassword: vi.fn(),
  }),
}));

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

describe('LoginView', () => {
  it('explains the pending setup instead of offering a sign-in form nobody can use', async () => {
    setupStatus.mockResolvedValue({ setupRequired: true });
    const wrapper = mount(LoginView);
    await settle();

    // A fresh installation has no accounts, so a sign-in form is a dead end.
    expect(wrapper.text()).toContain('has not been set up');
    expect(wrapper.text()).toContain('market-lens auth setup-link');
    expect(wrapper.find('input[type="email"]').exists()).toBe(false);
  });

  it('offers sign-in once setup is closed', async () => {
    setupStatus.mockResolvedValue({ setupRequired: false });
    const wrapper = mount(LoginView);
    await settle();

    expect(wrapper.text()).not.toContain('has not been set up');
    expect(wrapper.find('input[type="email"]').exists()).toBe(true);
  });

  it('falls back to the sign-in form when setup state cannot be read', async () => {
    setupStatus.mockRejectedValue(new Error('unavailable'));
    const wrapper = mount(LoginView);
    await settle();

    // An unreachable status endpoint must not strand somebody who does have an account.
    expect(wrapper.find('input[type="email"]').exists()).toBe(true);
  });
});
