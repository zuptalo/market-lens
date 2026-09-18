import { describe, expect, it } from 'vitest';
import { formatDate, formatDateTime } from './datetime';

/**
 * Every date this product shows is written the Swedish way: 2026-11-28, largest unit first.
 *
 * It was showing 9/18/2026, 7:41:06 AM, because `toLocaleString()` with no locale takes the
 * browser's — so the same recorded session read differently depending on whose phone opened it,
 * and on a Swedish product an American ordering is ambiguous as well as wrong: 9/18 is unreadable
 * to anybody who expects day first.
 */
describe('dates and times', () => {
  it('writes a date largest unit first, zero padded', () => {
    expect(formatDate('2026-11-28T09:15:00Z', 'UTC')).toBe('2026-11-28');
    expect(formatDate('2026-01-05T23:59:59Z', 'UTC')).toBe('2026-01-05');
  });

  it('writes a time on the 24 hour clock, with no seconds and no meridiem', () => {
    expect(formatDateTime('2026-09-18T07:41:06Z', 'UTC')).toBe('2026-09-18 07:41');
    expect(formatDateTime('2026-09-18T19:04:12Z', 'UTC')).toBe('2026-09-18 19:04');
  });

  // The reader is in their own timezone and the server speaks UTC. A stamp shown in UTC while
  // the clock on the wall says something else is a small, constant lie.
  it('reads the stamp in the timezone asked for', () => {
    expect(formatDateTime('2026-09-18T23:30:00Z', 'Europe/Stockholm')).toBe('2026-09-19 01:30');
    expect(formatDate('2026-09-18T23:30:00Z', 'Europe/Stockholm')).toBe('2026-09-19');
  });

  // A session date is already a date. Turning it into a Date and back has moved it across a day
  // boundary before, west of Greenwich.
  it('leaves a plain session date exactly as it is', () => {
    expect(formatDate('2026-11-28', 'America/New_York')).toBe('2026-11-28');
    expect(formatDate('2026-01-01', 'Pacific/Auckland')).toBe('2026-01-01');
  });

  it('says nothing rather than Invalid Date', () => {
    expect(formatDate(null)).toBe('—');
    expect(formatDateTime(undefined)).toBe('—');
    expect(formatDate('not a date')).toBe('—');
  });
});

describe('no screen formats its own dates', () => {
  it('leaves date formatting to this module', async () => {
    const { readFileSync, readdirSync, statSync } = await import('node:fs');
    const { join } = await import('node:path');

    const files = (directory: string): string[] => readdirSync(directory).flatMap((entry) => {
      const path = join(directory, entry);
      if (statSync(path).isDirectory()) return files(path);
      return /\.(vue|ts)$/.test(path) && !/\.test\.ts$/.test(path) ? [path] : [];
    });

    const root = join(process.cwd(), 'src');
    const offenders = files(root)
      .filter((path) => !path.endsWith(join('utils', 'datetime.ts')))
      .filter((path) => {
        const source = readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
        // A number may be formatted for the reader's locale — 1 234 567 is right either way.
        // A date may not: the ordering changes, and then two people read different days.
        return /new Date\([^)]*\)\s*\.\s*toLocale(String|DateString|TimeString)/.test(source);
      })
      .map((path) => path.slice(root.length + 1));

    expect(offenders, `these format a date in the reader's locale:\n${offenders.join('\n')}`)
      .toEqual([]);
  });
});
