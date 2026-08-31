import { describe, expect, it } from 'vitest';
import { formatDecimal } from './decimal';

/**
 * Money arrives as a decimal string from `numeric(20,8)`, so a price reads
 * `21540.00000000 DKK` unless something trims it. The trimming has to be careful: dropping
 * trailing zeros is safe because they carry no information, but dropping a significant digit
 * would state a price the data does not support.
 *
 * Everything here operates on the string. Parsing to a JavaScript number would silently lose
 * precision on exactly the values that matter most.
 */
describe('formatDecimal', () => {
  it('drops the trailing zeros that carry no information', () => {
    expect(formatDecimal('21540.00000000')).toBe('21540.00');
    expect(formatDecimal('109.50000000')).toBe('109.50');
    expect(formatDecimal('-430.00000000')).toBe('-430.00');
  });

  it('keeps every digit that is not a trailing zero', () => {
    // A price genuinely quoted to eight places keeps all eight. Rounding here would state a
    // number the provider did not report.
    expect(formatDecimal('0.12345678')).toBe('0.12345678');
    expect(formatDecimal('1.23450000')).toBe('1.2345');
    expect(formatDecimal('99.99999999')).toBe('99.99999999');
  });

  it('pads to two places so a price does not read as a count', () => {
    expect(formatDecimal('21540')).toBe('21540.00');
    expect(formatDecimal('7.5')).toBe('7.50');
  });

  it('honours a different minimum for values that are not money', () => {
    // A 2-for-1 split ratio is 2, not 2.00.
    expect(formatDecimal('2.00000000', 0)).toBe('2');
    expect(formatDecimal('1.50000000', 0)).toBe('1.5');
    expect(formatDecimal('3', 0)).toBe('3');
  });

  it('never loses precision that a JavaScript number could not hold', () => {
    // 20 significant digits: Number() would round this and quietly change the value.
    expect(formatDecimal('12345678901234.56789012')).toBe('12345678901234.56789012');
  });

  it('passes through anything that is not a plain decimal rather than mangling it', () => {
    expect(formatDecimal('')).toBe('');
    expect(formatDecimal('n/a')).toBe('n/a');
  });
});
