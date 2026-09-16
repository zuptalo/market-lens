import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * The charting library is confined to the chart components, and the list is exhaustive.
 *
 * That containment is the entire mitigation for adopting a third-party library at the centre of a
 * feature: if the licence becomes unacceptable, or the library is abandoned, or its API changes,
 * a known, short list of files has to change. The list is what makes that true — an import that
 * drifted into a view or a service would go quietly, in a commit that looked like a small
 * convenience, and the promise would be gone before anybody noticed.
 *
 * Feature 021 added the second entry. A backtest's equity curve is a second chart, not a chart
 * drawn somewhere new, and adding it here was a deliberate decision rather than a side effect —
 * which is the whole reason this list is asserted instead of described.
 */
const ALLOWED = [
  'src/components/finance/EquityCurve.vue',
  'src/components/finance/PriceChart.vue',
];

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

  it('keeps the licence-required attribution in every file that draws one', () => {
    // Apache-2.0 with an attribution requirement. Removing this is a licence breach, not a
    // styling choice, so it is asserted rather than left to review — on every chart, because the
    // requirement attaches to the chart and not to the first file that happened to draw one.
    for (const path of ALLOWED) {
      expect(readFileSync(path, 'utf8')).toContain('attributionLogo: true');
    }
  });
});
