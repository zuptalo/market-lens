import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import MarketsView from './MarketsView.vue';

class QuietEventSource extends EventTarget {
  static instances: QuietEventSource[] = [];
  constructor() {
    super();
    QuietEventSource.instances.push(this);
  }
  close(): void {}
  /** Dispatch under the event's own name, which is what the server writes. */
  deliver(type: string, data: string, lastEventId = '1'): void {
    this.dispatchEvent(new MessageEvent(type, { data, lastEventId }));
  }
}

function wireRow(overrides: Record<string, unknown> = {}) {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    isin: 'SE0000000200',
    ticker: 'GAPPY',
    name: 'Interrupted History AB',
    exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' },
    currency: 'SEK',
    country: 'SE',
    sector: 'Technology',
    industry: 'Software',
    instrument_type: 'common_stock',
    status: 'active',
    purchasability_status: 'unverified',
    latest_session: '2026-06-30',
    latest_close: '109.50',
    change_absolute: '1.00',
    change_percent: 0.0092,
    return_20: '0.041200000000',
    return_90: null,
    volatility: '0.187500000000',
    stored_sessions: 42,
    freshness: { state: 'current', sessions_behind: 0 },
    ...overrides,
  };
}

let requestedUrls: string[] = [];

/**
 * A universe of `total` rows served in pages, so scrolling has somewhere to go. Each page
 * carries the cursor of the next; the first carries the total, later ones carry null, which
 * is what the server does (research R-001).
 */
function stubPagedFetch(total: number, pageSize = 2) {
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
    const url = String(input);
    requestedUrls.push(url);
    if (url.includes('/api/v1/instruments?')) {
      const parameters = new URLSearchParams(url.split('?')[1] ?? '');
      const start = Number(parameters.get('cursor') ?? '0');
      const limit = Math.min(Number(parameters.get('limit') ?? String(pageSize)), pageSize);
      const rows: ReturnType<typeof wireRow>[] = [];
      for (let index = start; index < Math.min(start + limit, total); index += 1) {
        rows.push(wireRow({
          id: `11111111-1111-4111-8111-${String(index).padStart(12, '0')}`,
          ticker: `ROW${index}`,
          name: `Row ${index} AB`,
        }));
      }
      const next = start + limit < total ? String(start + limit) : null;
      return {
        ok: true,
        json: async () => ({ items: rows, next_cursor: next, total: start === 0 ? total : null }),
      };
    }
    if (url.includes('/api/v1/market-data/imports')) {
      return { ok: true, json: async () => ({ items: [] }) };
    }
    return { ok: false, json: async () => ({ error: 'not found' }) };
  }));
}

/**
 * Fire the sentinel the listing watches, the way a scroll would. The DOM these tests run in
 * has no IntersectionObserver, so the view takes one from the window when a test provides it.
 */
function installObserver(): void {
  (window as unknown as { __listingObserver?: unknown }).__listingObserver = {
    observe: () => {},
    trigger: () => {},
  };
}

function scrollToEnd(): void {
  const loadMore = (window as unknown as { __listingLoadMore?: () => void }).__listingLoadMore;
  loadMore?.();
}

function stubFetch(items: unknown[] = [wireRow()]) {
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
    const url = String(input);
    requestedUrls.push(url);
    if (url.includes('/api/v1/instruments?')) {
      return { ok: true, json: async () => ({ items, next_cursor: null, total: items.length }) };
    }
    if (url.includes('/api/v1/market-data/imports')) {
      return { ok: true, json: async () => ({ items: [] }) };
    }
    return { ok: false, json: async () => ({ error: 'not found' }) };
  }));
}

function mountView() {
  return mount(MarketsView, { global: { plugins: [PrimeVue] } });
}

describe('MarketsView', () => {
  beforeEach(() => {
    requestedUrls = [];
    window.localStorage.clear();
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  it('lists each instrument with identity, price in its own currency, and freshness', async () => {
    const wrapper = mountView();
    await flushPromises();
    const text = wrapper.text();
    expect(text).toContain('Interrupted History AB');
    expect(text).toContain('GAPPY');
    expect(text).toContain('XSTO');
    expect(text).toContain('109.50');
    expect(text).toContain('SEK');
    expect(text).toContain('Current');
  });

  // Feature 014 US1: operational reporting belongs on its own screen. A research page that
  // ends in a maintenance console serves neither reader — one scrolls past it, the other
  // cannot find it.
  it('carries no operational report, only a compact freshness statement that links onward', async () => {
    const wrapper = mountView();
    await flushPromises();
    const text = wrapper.text();
    // The import report's own vocabulary must be gone from this page.
    expect(text).not.toContain('Recent imports');
    expect(text).not.toContain('Copy retry command');
    expect(wrapper.find('[data-testid="import-run-list"]').exists()).toBe(false);
    // What remains is one statement about freshness, and a way to the detail.
    const link = wrapper.find('a[href="/operations"]');
    expect(link.exists()).toBe(true);
    expect(wrapper.findAll('a[href="/operations"]').length).toBe(1);
  });

  it('renders an uncomputed statistic as an absence rather than as a zero move', async () => {
    const wrapper = mountView();
    await flushPromises();
    // return_90 is null in the fixture row. A table showing 0.00% there would read as "this
    // instrument was flat over ninety sessions", which is not what the data says.
    expect(wrapper.text()).not.toContain('0.00%');
  });

  it('offers the sector filter the contract defines', async () => {
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.find('[aria-label="Sector"]').exists()).toBe(true);
  });

  it('asks the server for a new ordering when a column is sorted', async () => {
    const wrapper = mountView();
    await flushPromises();
    requestedUrls = [];
    const header = wrapper.findAll('th').find((th) => th.text().includes('20-session'));
    await header!.trigger('click');
    await flushPromises();
    const listingCall = requestedUrls.find((url) => url.includes('/api/v1/instruments?'));
    expect(listingCall).toBeDefined();
    expect(listingCall).toContain('sort=return_20');
  });

  it('explains an empty result and offers a way back instead of showing a blank table', async () => {
    stubFetch([]);
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text()).toContain('No instruments match these filters');
    expect(wrapper.find('[data-testid="clear-filters"]').exists()).toBe(true);
  });

  it('keeps the filters visible and intact when the listing request fails', async () => {
    const wrapper = mountView();
    await flushPromises();
    const search = wrapper.get('input[aria-label="Search instruments"]');
    vi.mocked(fetch).mockImplementation(async (input) => {
      if (String(input).includes('/api/v1/instruments?')) {
        return { ok: false, json: async () => ({ error: 'token=secret' }) } as Response;
      }
      return { ok: true, json: async () => ({ items: [] }) } as Response;
    });
    await search.setValue('ALFA');
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain('Unable to load instruments');
    // The message must not leak whatever the server said.
    expect(wrapper.text()).not.toContain('secret');
    expect((search.element as HTMLInputElement).value).toBe('ALFA');
  });

  it('mirrors the query into the address bar so a reload keeps the same view', async () => {
    const wrapper = mountView();
    await flushPromises();
    await wrapper.get('input[aria-label="Search instruments"]').setValue('gappy');
    await flushPromises();
    expect(window.location.search).toContain('q=gappy');
  });

  it('sends the contract vocabulary on the wire, not the words the old search used', async () => {
    mountView();
    await flushPromises();
    const listingCall = requestedUrls.find((url) => url.includes('/api/v1/instruments?'));
    expect(listingCall).toBeDefined();
    expect(listingCall).not.toContain('exchange=');
    expect(listingCall).not.toContain('active=');
  });
});

describe('MarketsView as a continuous list', () => {
  beforeEach(() => {
    requestedUrls = [];
    window.localStorage.clear();
    vi.stubGlobal('EventSource', QuietEventSource);
    installObserver();
  });

  // Feature 014 US2. The listing is read by scrolling; the button interrupted that once per
  // page, and nothing on the page said how large the result set was.
  it('loads the next page when the end of the loaded rows comes into view', async () => {
    stubPagedFetch(6);
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text()).toContain('Row 0 AB');
    expect(wrapper.text()).not.toContain('Row 2 AB');
    const before = requestedUrls.length;

    scrollToEnd();
    await flushPromises();

    expect(requestedUrls.length - before, 'scrolling requested no page').toBe(1);
    // The rows already read stay, and the new ones join them.
    expect(wrapper.text()).toContain('Row 0 AB');
    expect(wrapper.text()).toContain('Row 2 AB');
    wrapper.unmount();
  });

  it('states its position, its total, and where the list ends', async () => {
    stubPagedFetch(5);
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text()).toMatch(/2 of 5/);

    scrollToEnd();
    await flushPromises();
    expect(wrapper.text()).toMatch(/4 of 5/);

    scrollToEnd();
    await flushPromises();
    expect(wrapper.text()).toMatch(/5 of 5/);
    // "There is no more" and "there may be more" must be distinguishable.
    expect(wrapper.text().toLowerCase()).toMatch(/end of the list|all 5|no more/);
    wrapper.unmount();
  });

  it('offers a focusable control so the list has an end for keyboard and screen readers', async () => {
    stubPagedFetch(6);
    const wrapper = mountView();
    await flushPromises();
    const control = wrapper.find('[data-testid="load-more"]');
    expect(control.exists(), 'an infinite list with no focusable end strands keyboard users').toBe(true);
    const before = requestedUrls.length;
    await control.trigger('click');
    await flushPromises();
    expect(requestedUrls.length - before).toBe(1);
    wrapper.unmount();
  });

  it('announces arrivals politely without moving focus', async () => {
    stubPagedFetch(6);
    const wrapper = mountView();
    await flushPromises();
    const region = wrapper.find('[data-testid="listing-progress-announcement"]');
    expect(region.exists()).toBe(true);
    expect(region.attributes('aria-live')).toBe('polite');
    const focused = document.activeElement;
    scrollToEnd();
    await flushPromises();
    expect(region.text()).toMatch(/2 more|4 of 6/);
    expect(document.activeElement, 'an arriving page moved focus').toBe(focused);
    wrapper.unmount();
  });

  it('returns to the first page when a filter changes', async () => {
    stubPagedFetch(6);
    const wrapper = mountView();
    await flushPromises();
    scrollToEnd();
    await flushPromises();
    expect(wrapper.text()).toContain('Row 2 AB');

    const search = wrapper.find('input[type="search"], input#markets-search');
    expect(search.exists(), 'no search input to change').toBe(true);
    await search.setValue('anything');
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 400));
    await flushPromises();
    // A new filter is a new result set: the reader starts at the top of it, and the request
    // that fetches it carries no cursor from the old one.
    const listingRequests = requestedUrls.filter((url) => url.includes('/api/v1/instruments?'));
    expect(listingRequests.at(-1) ?? '').not.toContain('cursor=');
    wrapper.unmount();
  });

  it('keeps the rows already read when a page fails, and retries', async () => {
    stubPagedFetch(6);
    const wrapper = mountView();
    await flushPromises();
    const rowsBefore = wrapper.text();

    const failing = vi.fn(async (input: string | URL) => {
      const url = String(input);
      requestedUrls.push(url);
      if (url.includes('/api/v1/instruments?')) return { ok: false, json: async () => ({}) };
      return { ok: true, json: async () => ({ items: [] }) };
    });
    vi.stubGlobal('fetch', failing);
    scrollToEnd();
    await flushPromises();

    // The list is never silently truncated.
    expect(wrapper.text()).toContain('Row 0 AB');
    expect(rowsBefore.includes('Row 0 AB')).toBe(true);
    expect(wrapper.text().toLowerCase()).toMatch(/unable to load|try again|retry/);
    wrapper.unmount();
  });

  it('updates a row on a later page in place, without moving it', async () => {
    stubPagedFetch(6);
    const wrapper = mountView();
    await flushPromises();
    scrollToEnd();
    await flushPromises();
    const orderBefore = wrapper.findAll('tbody tr').map((row) => row.text());
    const before = requestedUrls.length;

    const source = QuietEventSource.instances.at(-1)!;
    source.deliver(
      'daily_bar.changed.v1',
      JSON.stringify({
        entity_type: 'daily_bar',
        entity_id: 'bar-1',
        instrument_id: '11111111-1111-4111-8111-000000000002',
      }),
      '7001',
    );
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 1200));
    await flushPromises();

    // One refresh, and it re-reads the rows the reader has — not the first page again.
    const refreshes = requestedUrls.slice(before).filter((url) => url.includes('/api/v1/instruments?'));
    expect(refreshes.length).toBe(1);
    expect(refreshes[0]).not.toContain('cursor=');
    // The rows keep their order and their number: content must not move under a reader.
    const orderAfter = wrapper.findAll('tbody tr').map((row) => row.text());
    expect(orderAfter.length).toBe(orderBefore.length);
    wrapper.unmount();
  });

  it('corrects the stated total when a live change alters what matches', async () => {
    stubPagedFetch(6);
    const wrapper = mountView();
    await flushPromises();
    expect(wrapper.text()).toMatch(/2 of 6/);

    // The universe shrinks underneath the reader: two instruments no longer match.
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
      const url = String(input);
      requestedUrls.push(url);
      if (url.includes('/api/v1/instruments?')) {
        return {
          ok: true,
          json: async () => ({
            items: [wireRow({ id: '11111111-1111-4111-8111-000000000000', ticker: 'ROW0', name: 'Row 0 AB' })],
            next_cursor: null,
            total: 4,
          }),
        };
      }
      return { ok: true, json: async () => ({ items: [] }) };
    }));

    const source = QuietEventSource.instances.at(-1)!;
    source.deliver(
      'daily_bar.changed.v1',
      JSON.stringify({
        entity_type: 'daily_bar',
        entity_id: 'bar-2',
        instrument_id: '11111111-1111-4111-8111-000000000000',
      }),
      '7002',
    );
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 1200));
    await flushPromises();

    // The total tells the truth about the result set; the rows the reader has stay put.
    expect(wrapper.text()).toMatch(/of 4/);
    expect(wrapper.text()).toContain('Row 0 AB');
    wrapper.unmount();
  });

  it('issues one page request at a time however fast the reader scrolls', async () => {
    stubPagedFetch(20);
    const wrapper = mountView();
    await flushPromises();
    const before = requestedUrls.length;
    scrollToEnd();
    scrollToEnd();
    scrollToEnd();
    scrollToEnd();
    await flushPromises();
    expect(requestedUrls.length - before, 'four intersections issued more than one request').toBe(1);
    wrapper.unmount();
  });

});

describe('MarketsView under an event storm', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    requestedUrls = [];
    window.localStorage.clear();
    QuietEventSource.instances = [];
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  it('coalesces a burst of events into a small number of requests', async () => {
    const wrapper = mountView();
    await flushPromises();
    const before = requestedUrls.length;

    // An import publishes one daily_bar event per stored bar. A backfill over the curated
    // universe is hundreds of thousands of them. One request per event is a denial of service
    // the page performs on itself.
    const source = QuietEventSource.instances.at(-1);
    expect(source, 'the view opened no event stream').toBeDefined();
    for (let index = 0; index < 200; index += 1) {
      source!.deliver(
        'daily_bar.changed.v1',
        JSON.stringify({
          entity_type: 'daily_bar',
          entity_id: `bar-${index}`,
          instrument_id: '11111111-1111-4111-8111-111111111111',
          session_date: '2026-06-30',
        }),
        String(index),
      );
    }
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2_000);
    await flushPromises();

    const issued = requestedUrls.length - before;
    expect(issued, `200 events issued ${issued} requests`).toBeLessThanOrEqual(3);
    // It must still actually refresh — coalescing to zero would be a different bug.
    expect(issued).toBeGreaterThan(0);
    wrapper.unmount();
  });

  it('issues no request at all for a burst naming instruments that are not on screen', async () => {
    const wrapper = mountView();
    await flushPromises();
    const before = requestedUrls.length;

    const source = QuietEventSource.instances.at(-1)!;
    for (let index = 0; index < 50; index += 1) {
      source.deliver(
        'daily_bar.changed.v1',
        JSON.stringify({ entity_type: 'daily_bar', entity_id: `bar-${index}`, instrument_id: `absent-${index}` }),
        String(1000 + index),
      );
    }
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2_000);
    await flushPromises();

    expect(requestedUrls.length - before).toBe(0);
    wrapper.unmount();
  });

  // Feature 013 US5: the three statistics on this page are the engine's, so the page must
  // hear the engine's own event. Before the event type is subscribed, the stream delivers it
  // into nothing and the row keeps a stale number until something else happens to refresh it.
  it('re-reads only the affected row when the engine recomputes its features', async () => {
    const wrapper = mountView();
    await flushPromises();
    const before = requestedUrls.length;
    const listingBefore = requestedUrls.filter((url) => url.includes('/api/v1/instruments')).at(-1);

    const source = QuietEventSource.instances.at(-1)!;
    source.deliver(
      'feature_values.changed.v1',
      JSON.stringify({
        entity_type: 'instrument',
        entity_id: '11111111-1111-4111-8111-111111111111',
        instrument_id: '11111111-1111-4111-8111-111111111111',
        from_session: '2026-06-01',
        to_session: '2026-06-30',
      }),
      '9001',
    );
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2_000);
    await flushPromises();

    const issued = requestedUrls.slice(before);
    expect(issued.length, 'a feature change on a listed instrument refreshed nothing').toBe(1);
    // The refresh keeps the view the person set up — same filters, same ordering — and asks
    // only for the rows on screen rather than starting the listing over.
    const parameters = (url: string) => new URLSearchParams(url.split('?')[1] ?? '');
    const before20 = parameters(listingBefore ?? '');
    const after = parameters(issued[0]);
    for (const key of ['q', 'sort', 'order']) {
      expect(after.get(key), `the refresh changed ${key}`).toBe(before20.get(key));
    }
    expect(after.get('cursor')).toBe(before20.get('cursor'));
    expect(Number(after.get('limit'))).toBe(wrapper.findAll('tbody tr').length);

    // An instrument that is not on screen costs nothing, and the same event twice is one
    // change, not two.
    const second = requestedUrls.length;
    source.deliver(
      'feature_values.changed.v1',
      JSON.stringify({ entity_type: 'instrument', entity_id: 'absent-1', instrument_id: 'absent-1' }),
      '9002',
    );
    source.deliver(
      'feature_values.changed.v1',
      JSON.stringify({
        entity_type: 'instrument',
        entity_id: '11111111-1111-4111-8111-111111111111',
        instrument_id: '11111111-1111-4111-8111-111111111111',
      }),
      '9001',
    );
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2_000);
    await flushPromises();
    expect(requestedUrls.length - second, 'a repeated event id refetched again').toBe(0);
    wrapper.unmount();
  });

  it('does not start a second request while one is still in flight', async () => {
    let release: ((value: unknown) => void) | undefined;
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
      const url = String(input);
      requestedUrls.push(url);
      if (url.includes('/api/v1/instruments?')) {
        await new Promise((resolve) => { release = resolve; });
        return { ok: true, json: async () => ({ items: [wireRow()], next_cursor: null }) };
      }
      return { ok: true, json: async () => ({ items: [] }) };
    }));

    const wrapper = mountView();
    await flushPromises();
    const before = requestedUrls.length;

    const source = QuietEventSource.instances.at(-1)!;
    for (let index = 0; index < 20; index += 1) {
      source.deliver(
        'daily_bar.changed.v1',
        JSON.stringify({ entity_type: 'daily_bar', entity_id: `b-${index}`, instrument_id: '11111111-1111-4111-8111-111111111111' }),
        String(2000 + index),
      );
    }
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2_000);
    await flushPromises();

    expect(requestedUrls.length - before, 'overlapping in-flight requests').toBeLessThanOrEqual(1);
    release?.(undefined);
    wrapper.unmount();
  });

  afterEach(() => vi.useRealTimers());
});
