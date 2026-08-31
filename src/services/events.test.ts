import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AuthorizedEventStream, type EventSourceLike, type StreamEvent } from './events';

describe('authorized event stream', () => {
  beforeEach(() => vi.useFakeTimers());

  it('deduplicates safe invalidations, resumes after errors, and exposes stale/offline states', async () => {
    const sources: FakeSource[] = [];
    const invalidations = vi.fn();
    const states: string[] = [];
    const stream = new AuthorizedEventStream({
      sourceFactory: (url) => {
        const source = new FakeSource(url);
        sources.push(source);
        return source;
      },
      reconnectDelayMs: 1_000,
      staleAfterMs: 10_000,
    });
    stream.configure({ onInvalidate: invalidations, onState: (state) => states.push(state), onUnauthorized: vi.fn() });

    stream.start();
    sources[0].open();
    sources[0].namedEvent('41', 'account.changed.v1', JSON.stringify({
      version: 1, scope: 'user', entity_type: 'account', entity_id: 'owner-id', payload: {}, occurred_at: '2026-08-30T08:00:00Z',
    }));
    sources[0].namedEvent('41', 'account.changed.v1', JSON.stringify({
      version: 1, scope: 'user', entity_type: 'account', entity_id: 'owner-id', payload: {}, occurred_at: '2026-08-30T08:00:00Z',
    }));
    expect(invalidations).toHaveBeenCalledOnce();

    sources[0].error();
    expect(states.at(-1)).toBe('reconnecting');
    await vi.advanceTimersByTimeAsync(1_000);
    expect(sources[1].url).toBe('/api/v1/events?last_event_id=41');
    await vi.advanceTimersByTimeAsync(9_000);
    expect(states.at(-1)).toBe('stale');

    stream.setOnline(false);
    expect(states.at(-1)).toBe('offline');
    stream.stop();
  });

  it('ignores malformed, unknown-scope, and owner-only events for member defense in depth', () => {
    const source = new FakeSource('');
    const invalidations = vi.fn();
    const stream = new AuthorizedEventStream({ sourceFactory: () => source, role: 'member' });
    stream.configure({ onInvalidate: invalidations, onState: vi.fn(), onUnauthorized: vi.fn() });
    stream.start();

    source.message('1', 'account.changed.v1', '{broken');
    source.message('2', 'account.changed.v1', JSON.stringify({ scope: 'private', entity_type: 'account', entity_id: 'other' }));
    source.message('3', 'setup.changed.v1', JSON.stringify({ scope: 'owner', entity_type: 'setup', entity_id: 'singleton' }));
    expect(invalidations).not.toHaveBeenCalled();
  });

  it('drops user-scoped events addressed to another account', () => {
    const source = new FakeSource('');
    const invalidations = vi.fn();
    const stream = new AuthorizedEventStream({ sourceFactory: () => source });
    stream.setAudience({ userId: 'member-a', role: 'member' });
    stream.configure({ onInvalidate: invalidations, onState: vi.fn(), onUnauthorized: vi.fn() });
    stream.start();

    // The server already scopes replay; the client refuses to act on anything addressed
    // elsewhere so a server-side mistake cannot invalidate the wrong person's snapshot.
    source.message('1', 'session.revoked.v1', JSON.stringify({
      scope: 'user', subject_user_id: 'member-b', entity_type: 'session', entity_id: 'session-b',
    }));
    expect(invalidations).not.toHaveBeenCalled();

    source.message('2', 'session.revoked.v1', JSON.stringify({
      scope: 'user', subject_user_id: 'member-a', entity_type: 'session', entity_id: 'session-a',
    }));
    expect(invalidations).toHaveBeenCalledOnce();

    // Shared market data carries no subject and reaches every signed-in member.
    source.message('3', 'daily_bar.changed.v1', JSON.stringify({
      scope: 'shared', entity_type: 'daily_bar', entity_id: 'bar-1',
    }));
    expect(invalidations).toHaveBeenCalledTimes(2);
  });

  it('gives up on an unauthorized stream instead of reconnecting into a refusal loop', async () => {
    const sources: FakeSource[] = [];
    const unauthorized = vi.fn();
    const stream = new AuthorizedEventStream({
      sourceFactory: (url) => {
        const source = new FakeSource(url);
        sources.push(source);
        return source;
      },
      reconnectDelayMs: 1_000,
    });
    stream.setAudience({ userId: 'member-a', role: 'member' });
    stream.configure({ onInvalidate: vi.fn(), onState: vi.fn(), onUnauthorized: unauthorized });
    stream.start();
    sources[0].open();

    // A session revoked or deactivated behind an open stream ends it for good.
    sources[0].refused(401);
    expect(unauthorized).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(sources).toHaveLength(1);
  });

  it('keeps snapshots current from the stream alone and never schedules a poll', () => {
    const interval = vi.spyOn(globalThis, 'setInterval');
    const source = new FakeSource('');
    const stream = new AuthorizedEventStream({ sourceFactory: () => source });
    stream.setAudience({ userId: 'member-a', role: 'member' });
    stream.configure({ onInvalidate: vi.fn(), onState: vi.fn(), onUnauthorized: vi.fn() });
    stream.start();
    source.open();

    expect(interval).not.toHaveBeenCalled();
    stream.stop();
    interval.mockRestore();
  });
});

class FakeSource implements EventSourceLike {
  private listeners = new Map<string, Array<(event: StreamEvent) => void>>();
  constructor(readonly url: string) {}
  addEventListener(type: string, listener: (event: StreamEvent) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }
  close(): void {}
  open(): void { this.emit('open', { lastEventId: '', type: 'open', data: '' }); }
  error(): void { this.emit('error', { lastEventId: '', type: 'error', data: '' }); }
  refused(status: number): void { this.emit('error', { lastEventId: '', type: 'error', data: '', status }); }
  message(lastEventId: string, type: string, data: string): void { this.emit('message', { lastEventId, type, data }); }
  namedEvent(lastEventId: string, type: string, data: string): void { this.emit(type, { lastEventId, type, data }); }
  private emit(type: string, event: StreamEvent): void {
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }
}
