import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import MemberList from './MemberList.vue';
import type { Member } from '@/types/auth';

function member(overrides: Partial<Member> = {}): Member {
  return {
    id: '10000000-0000-4000-8000-000000000601',
    email: 'member@example.com',
    displayName: 'Member One',
    status: 'active',
    loginState: 'available',
    blockedUntil: null,
    lockedAt: null,
    activeSessionCount: 1,
    createdAt: '2026-08-30T10:00:00Z',
    ...overrides,
  };
}

describe('MemberList', () => {
  it('reports an empty roster instead of rendering an empty table', () => {
    const wrapper = mount(MemberList, { props: { members: [] } });
    expect(wrapper.text()).toContain('No members yet');
    expect(wrapper.find('button[data-unlock]').exists()).toBe(false);
  });

  it('describes each login state accessibly and offers unlock only when locked', async () => {
    const wrapper = mount(MemberList, {
      props: {
        members: [
          member({ id: 'a0000000-0000-4000-8000-000000000001', loginState: 'available' }),
          member({
            id: 'a0000000-0000-4000-8000-000000000002', email: 'blocked@example.com',
            loginState: 'temporarily_blocked', blockedUntil: '2026-08-30T10:15:00Z',
          }),
          member({
            id: 'a0000000-0000-4000-8000-000000000003', email: 'locked@example.com',
            loginState: 'administratively_locked', lockedAt: '2026-08-30T10:00:00Z',
          }),
        ],
      },
    });

    const text = wrapper.text();
    expect(text).toContain('Available');
    expect(text).toContain('Temporarily blocked');
    expect(text).toContain('Locked');

    // Only the administratively locked member needs an owner action; a temporary block clears
    // itself, so offering unlock there would misrepresent what the owner is doing.
    const unlockButtons = wrapper.findAll('button[data-unlock]');
    expect(unlockButtons).toHaveLength(1);

    await unlockButtons[0].trigger('click');
    expect(wrapper.emitted('unlock')?.[0]).toEqual(['a0000000-0000-4000-8000-000000000003']);
  });

  it('exposes state as text rather than colour alone and labels each action', () => {
    const wrapper = mount(MemberList, {
      props: {
        members: [member({ loginState: 'administratively_locked', lockedAt: '2026-08-30T10:00:00Z' })],
      },
    });
    const unlock = wrapper.get('button[data-unlock]');
    // The accessible name must identify which member the action applies to.
    expect(unlock.attributes('aria-label')).toContain('member@example.com');
    expect(wrapper.get('[data-login-state]').text()).not.toBe('');
  });

  it('disables actions while an administration request is in flight', () => {
    const wrapper = mount(MemberList, {
      props: {
        members: [member({ loginState: 'administratively_locked', lockedAt: '2026-08-30T10:00:00Z' })],
        busy: true,
      },
    });
    expect(wrapper.get('button[data-unlock]').attributes('disabled')).toBeDefined();
  });

  it('offers deactivate for an active member and reactivate for a deactivated one', async () => {
    const wrapper = mount(MemberList, {
      props: {
        members: [
          member({ id: 'active-1', status: 'active' }),
          member({ id: 'inactive-1', email: 'gone@example.com', status: 'deactivated' }),
        ],
      },
    });

    const deactivate = wrapper.findAll('button[data-deactivate]');
    const reactivate = wrapper.findAll('button[data-reactivate]');
    expect(deactivate).toHaveLength(1);
    expect(reactivate).toHaveLength(1);
    expect(wrapper.text()).toContain('Deactivated');

    await deactivate[0].trigger('click');
    expect(wrapper.emitted('setStatus')?.[0]).toEqual([{ id: 'active-1', status: 'deactivated' }]);
    await reactivate[0].trigger('click');
    expect(wrapper.emitted('setStatus')?.[1]).toEqual([{ id: 'inactive-1', status: 'active' }]);
  });

  it('shows a safe error without leaking administration detail', () => {
    const wrapper = mount(MemberList, {
      props: { members: [], error: 'Member administration is temporarily unavailable.' },
    });
    expect(wrapper.get('[role="alert"]').text()).toBe('Member administration is temporarily unavailable.');
  });
});
