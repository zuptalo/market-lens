import { mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SessionList from './SessionList.vue';
import type { Session } from '@/types/auth';

const NOW = new Date('2026-09-03T07:00:00Z');

function session(overrides: Partial<Session> = {}): Session {
  return {
    id: 'ses-1',
    current: false,
    deviceLabel: 'Chrome on macOS',
    createdAt: '2026-09-03T05:26:00Z',
    lastSeenAt: '2026-09-03T05:27:00Z',
    idleExpiresAt: '2026-09-03T13:27:00Z',
    absoluteExpiresAt: '2026-10-03T05:26:00Z',
    revoked: false,
    ...overrides,
  };
}

function mountList(sessions: Session[]) {
  return mount(SessionList, { props: { sessions }, global: { plugins: [PrimeVue] } });
}

describe('SessionList', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(NOW); });
  afterEach(() => { vi.useRealTimers(); });

  it('lists a live session as a device that can be revoked', () => {
    const wrapper = mountList([session({ current: true })]);
    expect(wrapper.text()).toContain('Chrome on macOS');
    expect(wrapper.findAll('button').some((b) => b.text() === 'Revoke')).toBe(true);
  });

  // Production accumulated one of these per release: a session that can no longer authenticate,
  // never revoked because nothing was wrong with it, listed as a signed-in device with a Revoke
  // button. Somebody auditing their devices saw ten when they had one.
  it('does not present an idle-expired session as a signed-in device', () => {
    const expired = session({
      id: 'ses-old', deviceLabel: 'Chrome on an old day',
      lastSeenAt: '2026-09-02T21:37:00Z', idleExpiresAt: '2026-09-03T05:37:00Z',
    });
    const wrapper = mountList([session({ current: true }), expired]);
    const text = wrapper.text();
    expect(text).toContain('Chrome on macOS');
    expect(text).not.toContain('Chrome on an old day');
  });

  it('does not present a session past its absolute lifetime as a device', () => {
    const stale = session({
      id: 'ses-stale', deviceLabel: 'Chrome from last month',
      idleExpiresAt: '2026-09-04T00:00:00Z',
      absoluteExpiresAt: '2026-09-01T00:00:00Z',
    });
    const wrapper = mountList([session({ current: true }), stale]);
    expect(wrapper.text()).not.toContain('Chrome from last month');
  });

  it('does not present a revoked session as a device either', () => {
    const revoked = session({ id: 'ses-rev', deviceLabel: 'Chrome revoked', revoked: true });
    const wrapper = mountList([session({ current: true }), revoked]);
    expect(wrapper.text()).not.toContain('Chrome revoked');
  });

  // Saying nothing would be worse than saying too much: a person who expected to see their old
  // phone needs to know it is gone because it expired, not because the screen forgot it.
  it('says how many sessions ended rather than dropping them silently', () => {
    const wrapper = mountList([
      session({ current: true }),
      session({ id: 'a', idleExpiresAt: '2026-09-03T05:37:00Z' }),
      session({ id: 'b', revoked: true }),
    ]);
    expect(wrapper.text()).toMatch(/2 earlier sessions/);
  });

  it('explains an account whose only sessions have all ended', () => {
    const wrapper = mountList([session({ id: 'a', idleExpiresAt: '2026-09-03T05:37:00Z' })]);
    expect(wrapper.text().toLowerCase()).toContain('no device is currently signed in');
  });
});
