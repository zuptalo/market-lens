import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  MarketDataLive,
  fetchRecentImports,
  type LiveEvent,
  type LiveEventSource,
} from './marketData';

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
