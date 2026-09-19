import { describe, expect, it } from 'vitest';
import { describeDevice } from './device';

describe('describeDevice', () => {
  it('names the browser and the platform of a desktop Chrome session', () => {
    expect(describeDevice(
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) ' +
      'Chrome/150.0.0.0 Safari/537.36',
    )).toBe('Chrome on macOS');
  });

  // The phone in the screenshot that started this: six lines of Mozilla boilerplate for a
  // fact a person reads in two words.
  it('names an iPhone Safari session', () => {
    expect(describeDevice(
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) ' +
      'Version/27.0 Mobile/15E148 Safari/604.1',
    )).toBe('Safari on iPhone');
  });

  // Every Chromium browser claims to be Chrome, and Chrome claims to be Safari. Read the
  // most specific token first or every device on the screen is called Chrome.
  it('tells the Chromium family apart', () => {
    const windows = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) ';
    expect(describeDevice(`${windows}Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0`)).toBe('Edge on Windows');
    expect(describeDevice(`${windows}Chrome/149.0.0.0 Safari/537.36 OPR/120.0.0.0`)).toBe('Opera on Windows');
    expect(describeDevice(`${windows}Chrome/149.0.0.0 Safari/537.36`)).toBe('Chrome on Windows');
  });

  it('names the remaining platforms it can recognise', () => {
    expect(describeDevice('Mozilla/5.0 (iPad; CPU OS 18_7 like Mac OS X) Version/27.0 Safari/604.1'))
      .toBe('Safari on iPad');
    expect(describeDevice('Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Chrome/149.0.0.0 Mobile'))
      .toBe('Chrome on Android');
    expect(describeDevice('Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0'))
      .toBe('Firefox on Linux');
  });

  /**
   * A label nobody can parse is still evidence on a security screen. Dropping it would hide
   * exactly the session a person most needs to see: the one that does not look like a browser.
   */
  it('keeps what it has when it recognises only one half, or neither', () => {
    expect(describeDevice('Mozilla/5.0 (Windows NT 10.0; Win64; x64) SomeoneElsesBrowser/1.0')).toBe('Windows');
    expect(describeDevice('curl/8.7.1')).toBe('curl/8.7.1');
    expect(describeDevice('Unknown device')).toBe('Unknown device');
    expect(describeDevice('')).toBe('Unknown device');
  });
});
