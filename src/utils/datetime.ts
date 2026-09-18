/**
 * Dates and times, written the same way everywhere: 2026-11-28 and 2026-11-28 07:41.
 *
 * The ISO 8601 ordering is the Swedish standard and it is the one this product uses, for two
 * reasons. It sorts as text, which matters because half these values sit in tables. And it is
 * unambiguous: 09/11 is two different days depending on who is reading, while 2026-09-11 is one.
 *
 * Built from the parts rather than from `toLocaleString`, which takes the reader's locale when
 * none is given — so the same recorded session used to read differently on different phones.
 */

const ABSENT = '—';

/** A date that is already a date. A session date carries no time and must not be given one. */
const PLAIN_DATE = /^\d{4}-\d{2}-\d{2}$/;

function parts(value: string, timeZone: string): Record<string, string> | null {
  const when = new Date(value);
  if (Number.isNaN(when.getTime())) return null;
  const formatter = new Intl.DateTimeFormat('en-CA', {
    timeZone,
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hour12: false,
  });
  const found: Record<string, string> = {};
  for (const part of formatter.formatToParts(when)) found[part.type] = part.value;
  // Midnight comes back as 24 in some implementations; the day has already rolled, so the hour
  // is the only thing to correct.
  if (found.hour === '24') found.hour = '00';
  return found;
}

/**
 * The day a stamp falls on, in the reader's timezone.
 *
 * A value that is already a plain date is returned untouched. Parsing `2026-11-28` and formatting
 * it west of Greenwich yields the 27th, which is how a trade entered on one day comes to be shown
 * on another.
 */
export function formatDate(
  value: string | null | undefined,
  timeZone: string = Intl.DateTimeFormat().resolvedOptions().timeZone,
): string {
  if (!value) return ABSENT;
  if (PLAIN_DATE.test(value)) return value;
  const found = parts(value, timeZone);
  if (!found) return ABSENT;
  return `${found.year}-${found.month}-${found.day}`;
}

/**
 * A stamp, to the minute, on the 24 hour clock.
 *
 * Seconds are dropped deliberately: nothing a person reads on these screens is decided by them,
 * and they make every column wider for no one's benefit.
 */
export function formatDateTime(
  value: string | null | undefined,
  timeZone: string = Intl.DateTimeFormat().resolvedOptions().timeZone,
): string {
  if (!value) return ABSENT;
  const found = parts(value, timeZone);
  if (!found) return ABSENT;
  return `${found.year}-${found.month}-${found.day} ${found.hour}:${found.minute}`;
}
