import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createCoalescer } from './coalesce';

describe('createCoalescer', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('turns a burst into a single run carrying every key', async () => {
    const runs: Array<Set<string>> = [];
    const coalescer = createCoalescer((keys) => { runs.push(new Set(keys)); }, 300);

    for (let index = 0; index < 200; index += 1) coalescer.add(`instrument-${index % 5}`);
    expect(runs, 'ran before the window elapsed').toHaveLength(0);

    await vi.advanceTimersByTimeAsync(300);
    expect(runs).toHaveLength(1);
    // Five distinct instruments across two hundred events.
    expect(runs[0].size).toBe(5);
  });

  it('never runs immediately, so one event cannot outrun the batch', async () => {
    const runs: number[] = [];
    const coalescer = createCoalescer(() => { runs.push(1); }, 300);
    coalescer.add('a');
    expect(runs).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(300);
    expect(runs).toHaveLength(1);
  });

  it('keeps only one run in flight and folds later keys into the next one', async () => {
    const started: Array<Set<string>> = [];
    let release: (() => void) | undefined;
    const coalescer = createCoalescer(async (keys) => {
      started.push(new Set(keys));
      await new Promise<void>((resolve) => { release = resolve; });
    }, 300);

    coalescer.add('a');
    await vi.advanceTimersByTimeAsync(300);
    expect(started).toHaveLength(1);

    // More events arrive while the first run is still going.
    coalescer.add('b');
    coalescer.add('c');
    await vi.advanceTimersByTimeAsync(1_000);
    expect(started, 'a second run started while the first was in flight').toHaveLength(1);

    release?.();
    await vi.advanceTimersByTimeAsync(300);
    expect(started).toHaveLength(2);
    expect([...started[1]].sort()).toEqual(['b', 'c']);
  });

  it('stops after the backlog drains rather than rescheduling forever', async () => {
    const runs: number[] = [];
    const coalescer = createCoalescer(() => { runs.push(1); }, 300);
    coalescer.add('a');
    await vi.advanceTimersByTimeAsync(300);
    expect(runs).toHaveLength(1);
    // Nothing further is pending, so no further run may happen however long we wait.
    await vi.advanceTimersByTimeAsync(10_000);
    expect(runs).toHaveLength(1);
  });

  it('runs nothing after cancel, so an unmounted view issues no request', async () => {
    const runs: number[] = [];
    const coalescer = createCoalescer(() => { runs.push(1); }, 300);
    coalescer.add('a');
    coalescer.cancel();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(runs).toHaveLength(0);
    coalescer.add('b');
    await vi.advanceTimersByTimeAsync(1_000);
    expect(runs).toHaveLength(0);
  });

  it('recovers from a failing run instead of wedging', async () => {
    const runs: number[] = [];
    const coalescer = createCoalescer(async () => {
      runs.push(1);
      throw new Error('network');
    }, 300);

    coalescer.add('a');
    await vi.advanceTimersByTimeAsync(300).catch(() => {});
    await Promise.resolve().catch(() => {});
    expect(runs).toHaveLength(1);

    // A later burst must still be able to run: a thrown error must not leave the
    // in-flight flag stuck true forever.
    coalescer.add('b');
    await vi.advanceTimersByTimeAsync(300).catch(() => {});
    expect(runs.length).toBeGreaterThanOrEqual(2);
  });
});
