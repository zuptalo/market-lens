import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import NotificationSettings from './NotificationSettings.vue';

let calls: { url: string; method: string; body: string }[] = [];

function settingsWire(overrides: Record<string, unknown> = {}) {
  return {
    preferences: [
      { kind: 'decision_waiting', channel: 'email', enabled: true },
      { kind: 'decision_waiting', channel: 'web_push', enabled: true },
      { kind: 'paper_fill', channel: 'email', enabled: false },
      { kind: 'paper_fill', channel: 'web_push', enabled: false },
      { kind: 'signal_change', channel: 'email', enabled: false },
      { kind: 'signal_change', channel: 'web_push', enabled: false },
      { kind: 'release_deployed', channel: 'email', enabled: false },
      { kind: 'release_deployed', channel: 'web_push', enabled: false },
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
        { id: 'd1', label: "Kamran's phone", endpoint_digest: 'somebodyelsesmac',
          created_at: '2026-09-01T08:00:00Z', last_used_at: '2026-09-18T07:00:00Z' },
      ] }) };
    }
    if (url.includes('/notifications/history')) {
      return { ok: true, json: async () => ({ notifications: [] }) };
    }
    if (url.includes('/notifications/push-key')) {
      return { ok: true, json: async () => ({ public_key: 'BAAAAAAAAAAAAAAAAAAAAAAAAAAA' }) };
    }
    return { ok: true, json: async () => options.settings ?? settingsWire() };
  }));
}

const global = { plugins: [PrimeVue] };

/**
 * What the browser says about *this* device, which is a different question from what the account
 * prefers. The whole bug this suite now guards against was conflating the two: push was on for the
 * account, so the toggle read on, on a phone that had never been asked for permission and would
 * never receive anything.
 */
function stubBrowser(options: {
  available?: boolean;
  permission?: NotificationPermission;
  subscribed?: boolean;
} = {}) {
  const { available = true, permission = 'granted', subscribed = false } = options;
  if (!available) {
    vi.stubGlobal('Notification', undefined);
    vi.stubGlobal('navigator', { ...navigator, serviceWorker: undefined });
    return;
  }
  vi.stubGlobal('Notification', {
    permission,
    requestPermission: vi.fn(async () => permission),
  });
  vi.stubGlobal('PushManager', function PushManager() {});
  vi.stubGlobal('navigator', {
    ...navigator,
    serviceWorker: {
      register: vi.fn(async () => ({ pushManager: { subscribe: vi.fn(async () => localSubscription()) } })),
      getRegistration: vi.fn(async () => ({
        pushManager: { getSubscription: vi.fn(async () => (subscribed ? localSubscription() : null)) },
      })),
      ready: Promise.resolve({}),
    },
  });
}

function localSubscription() {
  return {
    endpoint: 'https://push.example.invalid/this-device',
    getKey: () => new Uint8Array(new ArrayBuffer(65)).buffer,
  };
}

describe('NotificationSettings', () => {
  beforeEach(() => {
    stubFetch();
    stubBrowser();
  });

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
    expect(switches.length).toBeGreaterThanOrEqual(8);
    const text = wrapper.text();
    for (const label of ['A decision is waiting', 'A paper order settled',
      'A strategy changed its view', 'Market Lens was updated']) {
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
    for (const forbidden of ['recommend', 'you should', 'stay informed', 'never miss']) {
      expect(text).not.toContain(forbidden);
    }
  });

  /**
   * The bug: push was on for the account, so this read "on" on a phone that had never been asked
   * for permission and would never receive a thing. An account preference and a device being
   * subscribed are different facts and the screen has to say both.
   */
  it('says when push is on for the account but not for this device', async () => {
    stubBrowser({ subscribed: false });
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();

    const text = wrapper.text().toLowerCase();
    expect(text).toContain('this device is not subscribed');
    expect(text).toContain('nothing will arrive here');
    // And the way out is right there, not further down the page.
    expect(wrapper.find('[data-testid="subscribe-this-device"]').exists()).toBe(true);
  });

  it('says nothing of the sort once this device is subscribed', async () => {
    stubBrowser({ subscribed: true });
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    expect(wrapper.text().toLowerCase()).not.toContain('this device is not subscribed');
  });

  /**
   * The moment somebody turns push on is the moment to ask the browser. Saving the preference and
   * saying nothing is what left a phone silently uncovered.
   */
  it('asks the browser for permission when push is turned on', async () => {
    stubBrowser({ subscribed: false });
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();

    const pushSwitches = wrapper.findAllComponents({ name: 'ToggleSwitch' })
      .filter((_, index) => index % 2 === 1);
    await pushSwitches[1].setValue(true);
    await flushPromises();

    expect(Notification.requestPermission).toHaveBeenCalled();
    expect(calls.some((call) => call.method === 'POST'
      && call.url.includes('/notifications/subscriptions'))).toBe(true);
  });

  it('does not ask again when this device is already subscribed', async () => {
    stubBrowser({ subscribed: true });
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();

    const pushSwitches = wrapper.findAllComponents({ name: 'ToggleSwitch' })
      .filter((_, index) => index % 2 === 1);
    await pushSwitches[1].setValue(true);
    await flushPromises();

    expect(Notification.requestPermission).not.toHaveBeenCalled();
  });

  /**
   * A refused permission cannot be re-asked: the browser answers "denied" without a prompt. Saying
   * "press subscribe" to somebody in that state is advice that cannot work.
   */
  it('says where to go when the browser has already refused', async () => {
    stubBrowser({ subscribed: false, permission: 'denied' });
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('refused');
    expect(text).toContain('browser settings');
  });

  // iOS Safari has no PushManager until the app is installed, which is not obvious from the screen.
  it('says what to do on a browser that cannot receive push at all', async () => {
    stubBrowser({ available: false });
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('home screen');
    expect(wrapper.find('[data-testid="subscribe-this-device"]').exists()).toBe(false);
  });

  // A list of devices is no use if you cannot find the one you are holding. The label is allowed to
  // arrive after the first render, so this waits for it rather than assuming one flush is enough.
  it('marks which listed device is this one', async () => {
    stubBrowser({ subscribed: true });
    stubFetch({ devices: [
      { id: 'd1', label: 'Mac, added 2026-09-18', endpoint_digest: 'somebodyelsesmac',
        created_at: '2026-09-18T20:31:48Z', last_used_at: null },
      { id: 'd2', label: 'iPhone, added 2026-09-19', endpoint_digest: 'thisdevicedigest',
        created_at: '2026-09-19T08:00:00Z', last_used_at: null },
    ] });
    const wrapper = mount(NotificationSettings, { global });
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('This device');
    });
  });

  it('reports a failure rather than showing every switch off', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) })));
    const wrapper = mount(NotificationSettings, { global });
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toContain('unable to load');
  });
});
