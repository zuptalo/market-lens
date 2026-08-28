import { describe, expect, it } from 'vitest';
import { formatBuildVersion } from './version';

describe('formatBuildVersion', () => {
  it('formats released semantic versions with a visible v prefix', () => {
    expect(formatBuildVersion('0.2.0')).toBe('v0.2.0');
    expect(formatBuildVersion('12.34.56')).toBe('v12.34.56');
  });

  it('identifies missing and local identities as development builds', () => {
    expect(formatBuildVersion('')).toBe('development');
    expect(formatBuildVersion('dev')).toBe('development');
    expect(formatBuildVersion('unknown')).toBe('development');
  });
});
