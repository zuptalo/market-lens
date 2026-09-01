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

/**
 * A percentage from a decimal string, computed on the digits.
 *
 * The feature engine stores its values as `numeric(24,12)` and serves them as strings for the
 * same reason prices are strings: a JavaScript number cannot hold every value the column can,
 * and a statistic that has been through a binary float is no longer the statistic the engine
 * computed. Multiplying by a hundred is a decimal-point shift, so it is done as one.
 */
export function formatDecimalPercent(value: string, fractionDigits = 2): string {
  if (!DECIMAL.test(value)) return value;

  const negative = value.startsWith('-');
  const [whole, fraction = ''] = value.replace('-', '').split('.');
  // Shift two places for the percentage, then round at the requested place, all in digits.
  const digits = whole + fraction.padEnd(fractionDigits + 3, '0');
  const point = whole.length + 2;
  const kept = digits.slice(0, point + fractionDigits);
  const next = digits[point + fractionDigits] ?? '0';

  let rounded = kept;
  if (next >= '5') {
    const carried = (BigInt(kept) + 1n).toString().padStart(kept.length, '0');
    rounded = carried;
  }
  const padded = rounded.padStart(fractionDigits + 1, '0');
  const integer = padded.slice(0, padded.length - fractionDigits).replace(/^0+(?=\d)/, '');
  const decimals = padded.slice(padded.length - fractionDigits);
  const printed = fractionDigits === 0 ? integer : `${integer}.${decimals}`;
  const zero = /^0(\.0*)?$/.test(printed);
  return `${negative && !zero ? '-' : ''}${printed}%`;
}
