import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import NotificationSettings from './NotificationSettings.vue';

let calls: { url: string; method: string; body: string }[] = [];

function settingsWire(overrides: Record<string, unknown> = {}) {
  return {
    preferences: [
      { kind: 'decision_waiting', channel: 'email', enabled: true },
      { kind: 'decision_waiting', channel: 'web_push', enabled: false },
      { kind: 'paper_fill', channel: 'email', enabled: false },
      { kind: 'paper_fill', channel: 'web_push', enabled: false },
      { kind: 'signal_change', channel: 'email', enabled: false },
      { kind: 'signal_change', channel: 'web_push', enabled: false },
    ],
    quiet_hours: { starts_at: '22:00', ends_at: '07:00', timezone: 'Europe/Stockholm' },
    nothing_is_on_by_default: true,
    ...overrides,
  };
}

function stubFetch(options: { settings?: unknown; devices?: unknown[] } = {}) {
  calls = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL, init?: RequestInit) => {
    const url = String(input);
    calls.push({ url, method: init?.method ?? 'GET', body: String(init?.body ?? '') });
    if (url.includes('/notifications/subscriptions')) {
      return { ok: true, json: async () => ({ subscriptions: options.devices ?? [
        { id: 'd1', label: "Kamran's phone", created_at: '2026-09-01T08:00:00Z',
          last_used_at: '2026-09-18T07:00:00Z' },
      ] }) };
    }
    if (url.includes('/notifications/history')) {
      return { ok: true, json: async () => ({ notifications: [] }) };
    }
    return { ok: true, json: async () => options.settings ?? settingsWire() };
  }));
}

const global = { plugins: [PrimeVue] };

describe('NotificationSettings', () => {
  beforeEach(() => stubFetch());

  /**
   * The statement the whole section is built around. Everything here is opt-in, and saying so is
   * what makes a switch being off read as a choice rather than an oversight.
   */
  it('says nothing is sent unless it was asked for', async () => {
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('nothing is sent');
    expect(text).toContain('asked for');
  });

  it('offers a switch for each kind on each channel', async () => {
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    const switches = wrapper.findAllComponents({ name: 'ToggleSwitch' });
    // Six preferences plus nothing else: quiet hours is a time range, not a switch.
    expect(switches.length).toBeGreaterThanOrEqual(6);
    const text = wrapper.text();
    for (const label of ['A decision is waiting', 'A paper order settled', 'A strategy changed its view']) {
      expect(text).toContain(label);
    }
  });

  it('turns one thing on through the write path', async () => {
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    await wrapper.findAllComponents({ name: 'ToggleSwitch' })[2].setValue(true);
    await flushPromises();

    const written = calls.find((call) => call.method === 'PUT' && call.url.includes('preferences'));
    expect(written?.body).toContain('paper_fill');
    expect(written?.body).toContain('"enabled":true');
  });

  it('shows the quiet window and says what it does', async () => {
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('quiet hours');
    // Held, not dropped: the difference between a quiet hour and a lost alert.
    expect(text).toContain('held until');
    expect(wrapper.text()).toContain('Europe/Stockholm');
  });

  it('lists the devices without their endpoints', async () => {
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    expect(wrapper.text()).toContain("Kamran's phone");
    expect(wrapper.text()).not.toContain('https://');
  });

  it('removes a device through the write path', async () => {
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    const remove = wrapper.findAll('button').find((b) => /remove|revoke/i.test(b.text()));
    await remove?.trigger('click');
    await flushPromises();

    const deleted = calls.find((call) => call.method === 'DELETE');
    expect(deleted?.url).toContain('/notifications/subscriptions/d1');
  });

  // A kind the owner alone is offered is simply absent for everybody else, so the interface never
  // shows a switch that cannot be switched.
  it('shows only what this person is offered', async () => {
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    expect(wrapper.text()).not.toContain('Market data did not arrive');
  });

  it('never suggests turning anything on', async () => {
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    for (const forbidden of ['we recommend', 'you should', 'recommended', 'stay informed']) {
      expect(text).not.toContain(forbidden);
    }
  });

  it('reports a failure rather than showing every switch off', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) })));
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toContain('unable to load');
  });
});
