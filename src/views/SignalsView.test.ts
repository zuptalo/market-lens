import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import SignalsView from './SignalsView.vue';
import { buildRankingWire, buildSignalWire } from '@/services/__fixtures__/marketData';

class RecordingEventSource extends EventTarget {
  static instances: RecordingEventSource[] = [];
  static named: string[] = [];
  closed = false;
  constructor(public readonly url: string) {
    super();
    RecordingEventSource.instances.push(this);
  }
  addEventListener(type: string, listener: EventListenerOrEventListenerObject | null): void {
    RecordingEventSource.named.push(type);
    super.addEventListener(type, listener);
  }
  close(): void { this.closed = true; }
}

const routerLinkStub = {
  props: ['to'],
  template: '<a :href="typeof to === \'string\' ? to : to.path"><slot /></a>',
};

function mountView() {
  return mount(SignalsView, {
    global: { plugins: [PrimeVue], stubs: { RouterLink: routerLinkStub } },
  });
}

function stubFetch(ranking: unknown = buildRankingWire()) {
  const calls: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
    calls.push(String(input));
    return { ok: true, status: 200, json: async () => ranking };
  }));
  return calls;
}

describe('SignalsView', () => {
  beforeEach(() => {
    RecordingEventSource.instances = [];
    RecordingEventSource.named = [];
    vi.stubGlobal('EventSource', RecordingEventSource);
    stubFetch();
  });

  it('lists the scored instruments in order with their action and score', async () => {
    const wrapper = mountView();
    await flushPromises();
    const ranking = wrapper.find('[data-testid="signal-ranking"]').text();
    expect(ranking).toContain('ALFA');
    expect(ranking).toContain('WATCH');
    expect(ranking).toContain('0.41');
    expect(wrapper.text()).toContain('momentum_trend');
    expect(wrapper.text()).toContain('2026-06-30');
  });

  // The separation is the claim. An instrument with no view must never be readable as the
  // weakest instrument in the ranking.
  it('separates the instruments it could not score and states why', async () => {
    const wrapper = mountView();
    await flushPromises();
    const ranking = wrapper.find('[data-testid="signal-ranking"]').text();
    expect(ranking).not.toContain('BETA');
    const unscored = wrapper.find('[data-testid="unscored-instruments"]').text();
    expect(unscored).toContain('BETA');
    expect(unscored.toLowerCase()).toContain('too little stored history');
  });

  it('links every row to that instrument, where the reasons are', async () => {
    const wrapper = mountView();
    await flushPromises();
    const link = wrapper.findAll('a').find((anchor) => anchor.text().includes('ALFA'));
    expect(link?.attributes('href')).toBe('/markets/dddddddd-0015-4000-8000-000000000001');
  });

  it('states that the ranking is a strategy output rather than advice', async () => {
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toContain('not advice');
    expect(wrapper.text()).toContain('stated rather than fitted');
  });

  // The server sends signals.changed.v1 as a *named* SSE event; a listener registered only for
  // 'message' receives nothing at all, and the failure is silent — connected, and never updated.
  it('subscribes to signals.changed.v1 by name and re-reads when one arrives', async () => {
    const calls = stubFetch();
    mountView();
    await flushPromises();
    expect(RecordingEventSource.named).toContain('signals.changed.v1');
    const before = calls.length;

    const source = RecordingEventSource.instances[0];
    const event = new MessageEvent('signals.changed.v1', {
      data: JSON.stringify({
        entity_type: 'instrument',
        entity_id: 'dddddddd-0015-4000-8000-000000000001',
        instrument_id: 'dddddddd-0015-4000-8000-000000000001',
        from_session: '2026-06-01',
        to_session: '2026-06-30',
      }),
      lastEventId: '42',
    });
    source.dispatchEvent(event);
    // The view coalesces a burst of events; one refresh follows within that window.
    await new Promise((resolve) => setTimeout(resolve, 400));
    await flushPromises();
    expect(calls.length).toBeGreaterThan(before);
  });

  it('explains a deployment where no strategy has run rather than showing an empty table', async () => {
    stubFetch(buildRankingWire({ items: [], scored: 0, unscored: 0, total: 0 }));
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toContain('no strategy has recorded');
    expect(wrapper.find('[data-testid="signal-ranking"]').exists()).toBe(false);
  });

  it('says so when the ranking cannot be loaded', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) })));
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text()).toContain('Unable to load the strategy ranking.');
  });

  it('keeps a single scored instrument grammatical', async () => {
    stubFetch(buildRankingWire({
      items: [{ ...buildSignalWire(), ticker: 'ALFA', name: 'Alfa AB', rank: 1 }],
      scored: 1, unscored: 0, total: 1,
    }));
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text()).toContain('1 instrument scored');
  });
});

describe('SignalsView while loading', () => {
  beforeEach(() => {
    RecordingEventSource.instances = [];
    vi.stubGlobal('EventSource', RecordingEventSource);
  });

  it('shows a first load where the ranking will be, not over its headings', async () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
    const wrapper = mountView();
    await flushPromises();

    expect(wrapper.find('[data-testid="loading-block"]').exists()).toBe(true);
    expect(wrapper.text()).toContain('Loading the ranking…');
    expect(wrapper.find('[data-testid="signal-ranking"]').exists()).toBe(false);
    // And it must not say no strategy has run while it is still asking.
    expect(wrapper.text().toLowerCase()).not.toContain('no strategy has recorded');
  });
});
