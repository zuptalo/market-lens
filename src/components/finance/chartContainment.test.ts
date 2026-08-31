import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * The charting library is confined to PriceChart.vue.
 *
 * That containment is the entire mitigation for adopting a third-party library at the centre
 * of the feature: if the licence becomes unacceptable, or the library is abandoned, or its
 * API changes, exactly one file has to change. The moment a second file imports it, that
 * promise is gone — and it would go quietly, in a commit that looked like a small convenience.
 */
const ALLOWED = ['src/components/finance/PriceChart.vue'];

function sourceFiles(directory: string, found: string[] = []): string[] {
  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry);
    if (statSync(path).isDirectory()) {
      if (entry === '__mocks__' || entry === 'node_modules') continue;
      sourceFiles(path, found);
    } else if (/\.(ts|vue)$/.test(entry)) {
      found.push(path);
    }
  }
  return found;
}

describe('charting library containment', () => {
  it('is imported by exactly one file', () => {
    const importers = sourceFiles('src')
      .filter((path) => !path.endsWith('.test.ts'))
      .filter((path) => /from\s+['"]lightweight-charts['"]/.test(readFileSync(path, 'utf8')))
      .map((path) => path.replace(/\\/g, '/'));

    expect(importers.sort()).toEqual(ALLOWED);
  });

  it('keeps the licence-required attribution in that file', () => {
    const source = readFileSync('src/components/finance/PriceChart.vue', 'utf8');
    // Apache-2.0 with an attribution requirement. Removing this is a licence breach, not a
    // styling choice, so it is asserted rather than left to review.
    expect(source).toContain('attributionLogo: true');
  });
});
