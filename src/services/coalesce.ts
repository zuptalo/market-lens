/**
 * Collapse a burst of invalidations into a small number of requests.
 *
 * The live stream is not paced for a reader. Feature 002's import publishes one
 * `daily_bar.changed.v1` per stored bar, so a backfill over the curated universe is hundreds
 * of thousands of events arriving as fast as the connection delivers them. A view that
 * refetches once per event performs a denial of service on itself: the browser opens a
 * request per event, none of them finish before the next arrives, and the page stops
 * responding — which is exactly what happened.
 *
 * So invalidations are collected rather than acted on. Within a short window they merge into
 * one set of affected keys, and at most one request is ever in flight. Anything that arrives
 * while a request is running is folded into the next one instead of racing it.
 *
 * The window is deliberately short. This is not throttling for its own sake — a person
 * watching a row should still see it change within about the time it takes to notice.
 */

export interface Coalescer {
  /** Note that `key` changed. Schedules a run; never runs immediately. */
  add(key: string): void;
  /** Drop anything pending and cancel the scheduled run. */
  cancel(): void;
}

export const DEFAULT_COALESCE_MS = 300;

export function createCoalescer(
  run: (keys: Set<string>) => Promise<void> | void,
  delayMs: number = DEFAULT_COALESCE_MS,
): Coalescer {
  let pending = new Set<string>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let inFlight = false;
  let cancelled = false;

  function schedule(): void {
    if (timer !== undefined || cancelled) return;
    timer = setTimeout(() => {
      timer = undefined;
      void flush();
    }, delayMs);
  }

  async function flush(): Promise<void> {
    if (cancelled) return;
    // A run is already going. Leave the keys pending; the run that is finishing will pick
    // them up, so this never stacks a second request behind the first.
    if (inFlight) {
      schedule();
      return;
    }
    if (pending.size === 0) return;

    const keys = pending;
    pending = new Set();
    inFlight = true;
    try {
      await run(keys);
    } catch {
      // Swallowed on purpose. A failed refresh is the caller's business — every view that
      // uses this already reports its own failure — and letting it escape here would surface
      // as an unhandled rejection from a timer, which is noise with no owner. What matters is
      // that the in-flight flag is cleared either way, so one network error cannot wedge the
      // view into never refreshing again.
    } finally {
      inFlight = false;
    }
    // Anything that arrived during the run gets one more pass, and only one.
    if (pending.size > 0) schedule();
  }

  return {
    add(key: string) {
      if (cancelled) return;
      pending.add(key);
      schedule();
    },
    cancel() {
      cancelled = true;
      if (timer !== undefined) clearTimeout(timer);
      timer = undefined;
      pending.clear();
    },
  };
}
