import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
	InstrumentSearchClient,
	MarketDataLive,
	fetchDailyPrices,
	fetchInstrument,
	fetchInstrumentListing,
	fetchRecentImports,
  type LiveEvent,
  type LiveEventSource,
} from './marketData';

describe('instrument snapshots', () => {
  it('maps typed search, detail, history, and empty results', async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ items: [{
        id: '33000000-0000-4000-8000-000000000001', isin: 'SE0000000100', ticker: 'ALFA',
        name: 'Alpha AB', exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm', timezone: 'Europe/Stockholm' },
        currency: 'SEK', country: 'SE', sector: 'Technology', industry: 'Software',
        instrument_type: 'common_stock', status: 'active', purchasability_status: 'unverified',
        latest_session: '2026-08-28', latest_close: '101.25', change_absolute: '1.25',
        change_percent: 0.0125, return_20: null, return_90: null, volatility: null,
        stored_sessions: 2, freshness: { state: 'current', sessions_behind: 0 },
      }], next_cursor: 'next' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({
        id: '33000000-0000-4000-8000-000000000001', isin: 'SE0000000100', ticker: 'ALFA', name: 'Alpha AB',
        exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm', timezone: 'Europe/Stockholm' }, currency: 'SEK',
        country: 'SE', instrument_type: 'common_stock', active: true, purchasability_status: 'unverified',
        latest_bar: { session_date: '2026-08-28', open: '100.125', high: '102.5', low: '99.75', close: '101.25', adjusted_close: null, volume: 1234, currency: 'SEK', provider: 'fixture', observed_at: '2026-08-29T18:30:00Z' },
        history: { first_session: '2026-08-27', last_session: '2026-08-28', bar_count: 2 },
        quality_summary: { open_warnings: 1, open_errors: 0 },
      }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ items: [], next_cursor: null }) });

    const page = await fetchInstrumentListing({ query: 'al fa', mic: 'XSTO', status: 'active', limit: 20 }, fetcher);
    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/instruments?q=al+fa&mic=XSTO&status=active&limit=20', expect.objectContaining({ signal: undefined }));
    expect(page.items[0]).toMatchObject({ ticker: 'ALFA', exchange: { mic: 'XSTO' } });
    expect(page.nextCursor).toBe('next');

    const detail = await fetchInstrument(page.items[0].id, fetcher);
    expect(detail.latestBar).toMatchObject({ sessionDate: '2026-08-28', close: '101.25' });
    expect(detail.history).toEqual({ firstSession: '2026-08-27', lastSession: '2026-08-28', barCount: 2 });
    expect(detail.qualitySummary.openWarnings).toBe(1);

    const history = await fetchDailyPrices(page.items[0].id, { from: '2026-08-01', to: '2026-08-28' }, fetcher);
    expect(history.items).toEqual([]);
  });

  it('cancels superseded searches and suppresses stale responses', async () => {
    const pending: Array<{ signal?: AbortSignal; resolve: (value: unknown) => void }> = [];
    const fetcher = vi.fn((_url: string, init?: RequestInit) => new Promise((resolve) => {
      pending.push({ signal: init?.signal ?? undefined, resolve });
    })) as never;
    const results: string[][] = [];
    const client = new InstrumentSearchClient(fetcher, (page) => results.push(page.items.map((item) => item.ticker)));

    const first = client.search({ query: 'old' });
    const second = client.search({ query: 'new' });
    expect(pending[0].signal?.aborted).toBe(true);
    pending[1].resolve({ ok: true, json: async () => ({ items: [{
      id: '33000000-0000-4000-8000-000000000002', isin: 'SE0000000200', ticker: 'NEW', name: 'New AB',
      exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm', timezone: 'Europe/Stockholm' }, currency: 'SEK',
      country: 'SE', sector: 'Technology', industry: 'Software', instrument_type: 'common_stock',
      status: 'active', purchasability_status: 'unverified', latest_session: '2026-08-28',
      latest_close: '101.25', change_absolute: '1.25', change_percent: 0.0125,
      return_20: null, return_90: null, volatility: null, stored_sessions: 2,
      freshness: { state: 'current', sessions_behind: 0 },
    }] }) });
    await second;
    pending[0].resolve({ ok: true, json: async () => ({ items: [] }) });
    await first;
    expect(results).toEqual([['NEW']]);

    client.cancel();
  });

  it('returns safe errors for failed instrument requests', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: false, json: async () => ({ error: 'token=secret' }) });
    await expect(fetchInstrument('33000000-0000-4000-8000-000000000001', fetcher)).rejects.toThrow('Unable to load instrument market data.');
  });
});

describe('market-data snapshots', () => {
  it('loads typed recent runs and rejects unsafe error payloads', async () => {
    const fetcher = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        items: [{
          id: '22000000-0000-4000-8000-000000000001',
          kind: 'backfill',
          provider: 'fixture',
          status: 'partial',
          started_at: '2026-08-29T08:00:00Z',
          finished_at: '2026-08-29T08:01:00Z',
          counts: { processed: 3, accepted: 2, rejected: 1, flagged: 1 },
          error_summary: 'One instrument failed.',
        }],
      }),
    });
    const runs = await fetchRecentImports(fetcher);
    expect(fetcher).toHaveBeenCalledWith('/api/v1/market-data/imports?limit=20', expect.objectContaining({ signal: undefined }));
    expect(runs).toHaveLength(1);
    expect(runs[0]).toMatchObject({ status: 'partial', counts: { accepted: 2, rejected: 1 } });

    fetcher.mockResolvedValueOnce({ ok: false, json: async () => ({ error: 'token=secret raw provider failure' }) });
    await expect(fetchRecentImports(fetcher)).rejects.toThrow('Unable to load recent market-data imports.');
  });

  it('never stores raw provider secrets from a successful snapshot payload', async () => {
    const fetcher = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ items: [{
        id: '22000000-0000-4000-8000-000000000001', kind: 'backfill', provider: 'fixture', status: 'failed',
        started_at: '2026-08-29T08:00:00Z', finished_at: '2026-08-29T08:01:00Z',
        counts: { processed: 0, accepted: 0, rejected: 0, flagged: 0 },
        error_summary: 'unauthorized api_token=ml-secret-t062-never-expose',
      }] }),
    });

    const runs = await fetchRecentImports(fetcher);
    expect(JSON.stringify(runs)).not.toContain('ml-secret-t062-never-expose');
    expect(runs[0].errorSummary).toBe('Market-data import failed.');
  });
});

describe('market-data live updates', () => {
  beforeEach(() => vi.useFakeTimers());

  it('refreshes duplicate-safely, reconnects from the last event ID, and exposes connection state', async () => {
    const sources: FakeSource[] = [];
    const states: string[] = [];
    const refresh = vi.fn();
    const live = new MarketDataLive({
      sourceFactory: (_url, lastEventId) => {
        const source = new FakeSource(lastEventId);
        sources.push(source);
        return source;
      },
      onRefresh: refresh,
      onState: (state) => states.push(state),
      reconnectDelayMs: 1_000,
      staleAfterMs: 10_000,
    });

    live.start();
    sources[0].open();
    expect(states.at(-1)).toBe('connected');
    sources[0].message({ lastEventId: '41', type: 'import_run.changed.v1', data: '{"entity_id":"run"}' });
    sources[0].message({ lastEventId: '41', type: 'import_run.changed.v1', data: '{"entity_id":"run"}' });
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(refresh).toHaveBeenCalledWith('import_run', 'run');

    sources[0].error();
    expect(states.at(-1)).toBe('reconnecting');
    await vi.advanceTimersByTimeAsync(1_000);
    expect(sources[1].lastEventId).toBe('41');
    await vi.advanceTimersByTimeAsync(9_000);
    expect(states.at(-1)).toBe('stale');
    live.setOnline(false);
    expect(states.at(-1)).toBe('offline');
    live.stop();
  });
});

class FakeSource implements LiveEventSource {
  readonly lastEventId: string;
  private listeners = new Map<string, Array<(event: LiveEvent) => void>>();

  constructor(lastEventId: string) {
    this.lastEventId = lastEventId;
  }

  addEventListener(type: string, listener: (event: LiveEvent) => void): void {
    const listeners = this.listeners.get(type) ?? [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  close(): void {}

  open(): void { this.emit('open', { lastEventId: '', type: 'open', data: '' }); }
  error(): void { this.emit('error', { lastEventId: '', type: 'error', data: '' }); }
  message(event: LiveEvent): void { this.emit('message', event); }

  private emit(type: string, event: LiveEvent): void {
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }
}
