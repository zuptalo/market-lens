import { describe, expect, it } from 'vitest';
import { movingAverage } from './movingAverage';
import { buildBars, MISSING_SESSIONS } from '@/services/__fixtures__/marketData';

/**
 * A moving average is a claim about the sessions behind each point. Two ways of breaking
 * that claim are easy to write and hard to see on a chart:
 *
 *  - starting the line at the left edge, where there are not yet enough prior sessions;
 *  - carrying it across a gap, which averages observations that do not exist.
 *
 * Both are forbidden by FR-012, and both are what these tests are for.
 */
describe('movingAverage', () => {
  it('is undefined until enough prior sessions exist', () => {
    const bars = buildBars();
    const series = movingAverage(bars, 5);
    expect(series).toHaveLength(bars.length);
    for (let index = 0; index < 4; index += 1) {
      expect(series[index].value).toBeNull();
    }
    expect(series[4].value).not.toBeNull();
  });

  it('computes the mean of the closing prices in its window', () => {
    const bars = buildBars(['2026-05-18', '2026-05-19', '2026-05-20']);
    // Closes are 100.50, 101.50, 102.50 by construction.
    const series = movingAverage(bars, 3);
    expect(series[2].value).toBeCloseTo((100.5 + 101.5 + 102.5) / 3, 10);
  });

  it('breaks at a gap instead of averaging across observations that do not exist', () => {
    const bars = buildBars();
    const series = movingAverage(bars, 3, MISSING_SESSIONS);

    // The fixture is missing 2026-05-25 and 2026-05-26. The first session after the gap is
    // 2026-05-27, and it has only itself behind it — the sessions before the gap are on the
    // far side of two absent observations and cannot contribute to a three-session mean.
    const afterGap = series.findIndex((point) => point.sessionDate === '2026-05-27');
    expect(afterGap).toBeGreaterThan(-1);
    expect(series[afterGap].value).toBeNull();
    expect(series[afterGap + 1].value).toBeNull();
    // Three sessions after the gap the window is satisfied again and the line resumes.
    expect(series[afterGap + 2].value).not.toBeNull();
  });

  it('spans nothing when the window is longer than the run of sessions available', () => {
    const bars = buildBars();
    const series = movingAverage(bars, 6, MISSING_SESSIONS);
    // The longest unbroken run in the fixture is five sessions, so a six-session average is
    // undefined everywhere rather than approximated.
    expect(series.every((point) => point.value === null)).toBe(true);
  });

  it('returns a point for every bar so the overlay stays aligned with the series', () => {
    const bars = buildBars();
    const series = movingAverage(bars, 3, MISSING_SESSIONS);
    expect(series.map((point) => point.sessionDate)).toEqual(bars.map((bar) => bar.sessionDate));
  });

  it('never invents a point on a session that has no bar', () => {
    const series = movingAverage(buildBars(), 3, MISSING_SESSIONS);
    for (const missing of MISSING_SESSIONS) {
      expect(series.some((point) => point.sessionDate === missing)).toBe(false);
    }
  });
});
