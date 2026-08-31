/**
 * Format a decimal string for display.
 *
 * Money crosses the wire as a decimal string, never a number, because the stored column is
 * `numeric(20,8)` and a JavaScript number cannot hold every value it can. That means a price
 * arrives as `21540.00000000` and reads as noise unless something trims it.
 *
 * The trimming removes trailing zeros and nothing else. Those zeros carry no information, so
 * dropping them changes nothing a reader can act on — while rounding a significant digit
 * would state a price the data does not support, which is the one thing this product does not
 * do anywhere else either.
 *
 * All of it operates on the string. Parsing to a number first would lose precision on exactly
 * the values where precision matters.
 */

/** A plain decimal: optional sign, digits, optional fractional part. */
const DECIMAL = /^-?\d+(\.\d+)?$/;

export function formatDecimal(value: string, minimumFractionDigits = 2): string {
  if (!DECIMAL.test(value)) return value;

  const [whole, fraction = ''] = value.split('.');
  const trimmed = fraction.replace(/0+$/, '');
  const padded = trimmed.padEnd(minimumFractionDigits, '0');
  return padded === '' ? whole : `${whole}.${padded}`;
}

/** A percentage from a unit fraction, e.g. 0.0092 → "0.92%". */
export function formatPercent(value: number, fractionDigits = 2): string {
  return `${(value * 100).toFixed(fractionDigits)}%`;
}
