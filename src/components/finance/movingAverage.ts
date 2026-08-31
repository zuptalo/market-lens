import type { Bar } from '@/types/marketData';

/**
 * A moving average over stored sessions, computed on the client from the bars it already
 * holds. Nothing about it is sent by the server, and nothing about it is stored.
 *
 * The overlay is a claim about the sessions behind each point, so it is undefined in two
 * cases and drawn in no others:
 *
 *  - before enough prior sessions exist, so the line begins where it becomes computable
 *    rather than at the left edge of the chart;
 *  - across a gap, because averaging over a missing session would average observations that
 *    do not exist.
 *
 * Both rules come from FR-012, and the second is the one that is easy to get wrong: a
 * naive window over the bars array simply skips the absent sessions and produces a
 * confident, wrong number.
 */

export interface OverlayPoint {
  sessionDate: string;
  /** Null where the average is undefined. The chart draws a break, not a zero. */
  value: number | null;
}

export function movingAverage(
  bars: Bar[],
  windowLength: number,
  missingSessions: string[] = [],
): OverlayPoint[] {
  const missing = new Set(missingSessions);

  // Sessions since the last interruption. A gap resets it, which is what makes the overlay
  // restart on the far side instead of stepping over the hole.
  let run = 0;
  let previousDate: string | null = null;

  return bars.map((bar, index) => {
    if (previousDate !== null && gapBetween(previousDate, bar.sessionDate, missing)) {
      run = 0;
    }
    previousDate = bar.sessionDate;
    run += 1;

    if (run < windowLength) return { sessionDate: bar.sessionDate, value: null };

    const slice = bars.slice(index + 1 - windowLength, index + 1);
    const total = slice.reduce((sum, entry) => sum + Number(entry.close), 0);
    return { sessionDate: bar.sessionDate, value: total / windowLength };
  });
}

/** Whether any session the exchange was open for sits between two consecutive stored bars. */
function gapBetween(previous: string, current: string, missing: Set<string>): boolean {
  for (const absent of missing) {
    if (absent > previous && absent < current) return true;
  }
  return false;
}
