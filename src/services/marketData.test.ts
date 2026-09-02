import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
	InstrumentSearchClient,
	MarketDataLive,
	fetchInstrument,
	fetchInstrumentListing,
	fetchRecentImports,
	fetchInstrumentSignal,
	fetchSignalRanking,
	fetchStrategyRuns,
	MARKET_DATA_EVENT_TYPES,
  type LiveEvent,
  type LiveEventSource,
} from './marketData';
import { buildRankingWire, buildSignalWire } from '@/services/__fixtures__/marketData';

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
    expect(refresh).toHaveBeenCalledWith('import_run', 'run', expect.objectContaining({ entity_id: 'run' }));

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

  it('subscribes by event name, because that is what the server sends', () => {
    const sources: FakeSource[] = [];
    const refresh = vi.fn();
    const live = new MarketDataLive({
      sourceFactory: (_url, lastEventId) => {
        const source = new FakeSource(lastEventId);
        sources.push(source);
        return source;
      },
      onRefresh: refresh,
      onState: () => {},
      reconnectDelayMs: 1_000,
      staleAfterMs: 10_000,
    });
    live.start();
    sources[0].open();

    // The server writes "event: <type>" for every event. A client listening only for
    // 'message' receives none of them, and the view silently never updates — which is the
    // failure mode this test exists to make impossible.
    for (const type of [
      'daily_bar.changed.v1', 'import_run.changed.v1', 'import_item.changed.v1',
      'quality_finding.changed.v1', 'corporate_action.changed.v1',
    ]) {
      expect(sources[0].subscribed(), `not subscribed to ${type}`).toContain(type);
    }

    sources[0].named({
      lastEventId: '7', type: 'daily_bar.changed.v1',
      data: '{"entity_id":"bar-1","instrument_id":"i-1","session_date":"2026-06-30"}',
    });
    expect(refresh).toHaveBeenCalledWith('daily_bar', 'bar-1', expect.objectContaining({
      instrument_id: 'i-1', session_date: '2026-06-30',
    }));

    // A corporate action must reach the view too: it was the one committed change the import
    // used to record silently.
    sources[0].named({
      lastEventId: '8', type: 'corporate_action.changed.v1',
      data: '{"entity_id":"a-1","instrument_id":"i-1","ex_date":"2026-05-28"}',
    });
    expect(refresh).toHaveBeenCalledWith('corporate_action', 'a-1', expect.objectContaining({
      instrument_id: 'i-1',
    }));
    live.stop();
  });

  // Feature 013 US5: a feature recomputation is a market-data change like any other, so the
  // stream must carry it by name and a reconnection must resume from where the drop happened —
  // replaying the events that were missed and none that were already applied.
  it('replays only the feature events missed while the stream was down', async () => {
    const sources: FakeSource[] = [];
    const refresh = vi.fn();
    const live = new MarketDataLive({
      sourceFactory: (_url, lastEventId) => {
        const source = new FakeSource(lastEventId);
        sources.push(source);
        return source;
      },
      onRefresh: refresh,
      onState: () => {},
      reconnectDelayMs: 1_000,
      staleAfterMs: 10_000,
    });
    live.start();
    sources[0].open();
    sources[0].named({
      lastEventId: '70', type: 'feature_values.changed.v1',
      data: '{"entity_type":"instrument","entity_id":"i-1","instrument_id":"i-1","from_session":"2026-06-01","to_session":"2026-06-30"}',
    });
    expect(refresh).toHaveBeenCalledWith('instrument', 'i-1', expect.objectContaining({ instrument_id: 'i-1' }));

    sources[0].error();
    await vi.advanceTimersByTimeAsync(1_000);
    // The reconnection asks the server to continue from the last event it applied.
    expect(sources[1].lastEventId).toBe('70');
    sources[1].open();
    // The server replays from there: the event already applied must not be applied twice, and
    // the one that happened during the drop must be.
    sources[1].named({
      lastEventId: '70', type: 'feature_values.changed.v1',
      data: '{"entity_type":"instrument","entity_id":"i-1","instrument_id":"i-1"}',
    });
    sources[1].named({
      lastEventId: '71', type: 'feature_values.changed.v1',
      data: '{"entity_type":"instrument","entity_id":"i-2","instrument_id":"i-2"}',
    });
    expect(refresh).toHaveBeenCalledTimes(2);
    expect(refresh).toHaveBeenLastCalledWith('instrument', 'i-2', expect.objectContaining({ instrument_id: 'i-2' }));
    live.stop();
  });

  it('applies a repeated event identifier exactly once even across event names', () => {
    const sources: FakeSource[] = [];
    const refresh = vi.fn();
    const live = new MarketDataLive({
      sourceFactory: (_url, lastEventId) => {
        const source = new FakeSource(lastEventId);
        sources.push(source);
        return source;
      },
      onRefresh: refresh,
      onState: () => {},
      reconnectDelayMs: 1_000,
      staleAfterMs: 10_000,
    });
    live.start();
    sources[0].open();
    const event = {
      lastEventId: '9', type: 'daily_bar.changed.v1',
      data: '{"entity_id":"bar-1","instrument_id":"i-1"}',
    };
    sources[0].named(event);
    sources[0].named(event);
    expect(refresh).toHaveBeenCalledTimes(1);
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
  /**
   * Dispatch under the event's own name, which is what the server actually sends: the SSE
   * writer emits "event: daily_bar.changed.v1", and a real EventSource routes that to a
   * listener registered for that name and never to the generic 'message' one.
   */
  named(event: LiveEvent): void { this.emit(event.type, event); }
  subscribed(): string[] { return [...this.listeners.keys()]; }

  private emit(type: string, event: LiveEvent): void {
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }
}

describe('signal reads', () => {
  it('maps a scored signal, keeping every decimal a string', async () => {
    const fetcher = vi.fn(async () => ({ ok: true, status: 200, json: async () => buildSignalWire() }));
    const signal = await fetchInstrumentSignal('dddddddd-0015-4000-8000-000000000001', {}, fetcher);
    expect(signal?.score).toBe('0.412500000000');
    expect(signal?.action).toBe('WATCH');
    expect(signal?.strategy.caveat).toContain('stated rather than fitted');
    expect(signal?.contributions[0]).toMatchObject({
      factor: 'momentum_90', feature: 'return_90',
      featureValue: '0.081234000000', featureSession: '2026-06-30',
      weight: '0.250000000000', contribution: '0.100000000000',
    });
  });

  // An instrument with no recorded view is an ordinary state of the world — a newly listed
  // company, a strategy that has not run — not an error the screen should shout about.
  it('returns null rather than throwing when no signal exists', async () => {
    const fetcher = vi.fn(async () => ({ ok: false, status: 404, json: async () => ({}) }));
    await expect(fetchInstrumentSignal('x', {}, fetcher)).resolves.toBeNull();
  });

  it('throws when the signal read fails for any other reason', async () => {
    const fetcher = vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) }));
    await expect(fetchInstrumentSignal('x', {}, fetcher)).rejects.toThrow();
  });

  it('forwards the session, strategy and version it was asked for', async () => {
    const requested: string[] = [];
    const fetcher = vi.fn(async (input: string) => {
      requested.push(input);
      return { ok: true, status: 200, json: async () => buildSignalWire() };
    });
    await fetchInstrumentSignal('abc', { asOf: '2026-06-30', strategy: 'momentum_trend', version: 2 }, fetcher);
    const url = requested[0];
    expect(url).toContain('as_of=2026-06-30');
    expect(url).toContain('strategy=momentum_trend');
    expect(url).toContain('version=2');
  });

  it('maps a ranking, separating what was scored from what was not', async () => {
    const fetcher = vi.fn(async () => ({ ok: true, status: 200, json: async () => buildRankingWire() }));
    const page = await fetchSignalRanking({}, fetcher);
    expect(page.scored).toBe(1);
    expect(page.unscored).toBe(1);
    expect(page.total).toBe(2);
    expect(page.sessionDate).toBe('2026-06-30');
    expect(page.items[0]).toMatchObject({ ticker: 'ALFA', rank: 1, action: 'WATCH' });
    expect(page.items[1]).toMatchObject({ ticker: 'BETA', rank: null, absenceReason: 'insufficient_history' });
  });

  it('maps strategy runs for the operational screen', async () => {
    const fetcher = vi.fn(async () => ({
      ok: true, status: 200, json: async () => ({ items: [{
        id: 'cccccccc-0015-4000-8000-000000000001', kind: 'incremental', status: 'partial',
        started_at: '2026-09-02T04:10:00Z', finished_at: '2026-09-02T04:10:26Z',
        instrument_count: 100, signal_count: 25_460, failed_count: 4,
        trigger_feature_run_id: 'bbbbbbbb-0013-4000-8000-000000000001', app_version: '0.12.0',
      }] }),
    }));
    const runs = await fetchStrategyRuns(fetcher);
    expect(runs[0]).toMatchObject({ status: 'partial', failedCount: 4, signalCount: 25_460 });
  });

  // The server writes signals.changed.v1 as a named SSE event; listening only for 'message'
  // receives nothing, with no error to notice.
  it('subscribes to signals.changed.v1 by name', () => {
    expect(MARKET_DATA_EVENT_TYPES).toContain('signals.changed.v1');
  });
});
