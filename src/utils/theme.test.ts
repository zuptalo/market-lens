import { describe, expect, it } from 'vitest';
import { isDarkTheme, normalizeTheme } from './theme';

describe('theme preference', () => {
  it('falls back to system for unknown stored values', () => {
    expect(normalizeTheme('legacy')).toBe('system');
    expect(normalizeTheme(null)).toBe('system');
  });

  it('resolves explicit and system dark mode', () => {
    expect(isDarkTheme('dark', false)).toBe(true);
    expect(isDarkTheme('light', true)).toBe(false);
    expect(isDarkTheme('system', true)).toBe(true);
  });
});
