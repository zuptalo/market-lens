import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

/**
 * A table has to be readable on a phone, and the way this project makes it readable is by turning
 * each row into a stack of labelled blocks below the tablet breakpoint.
 *
 * Both rules here exist because the first attempt at this was a prop that did nothing. Eleven
 * tables asked to stack with `responsive-layout="stack"`, which is PrimeVue 3's API; PrimeVue 4
 * removed it and passes the unknown prop straight through to the DOM as an attribute. Nothing
 * warned, nothing failed, and every finance table rendered six columns into 360 pixels for
 * months — the page either scrolled sideways or the table did, and on iOS Safari the first of
 * those shrinks the entire document to fit.
 */

function vueFiles(directory: string): string[] {
  return readdirSync(directory).flatMap((entry) => {
    const path = join(directory, entry);
    if (statSync(path).isDirectory()) return vueFiles(path);
    return path.endsWith('.vue') ? [path] : [];
  });
}

const root = join(process.cwd(), 'src');
// Prose that discusses the rule is not a use of it; only what is rendered counts.
function withoutComments(source: string): string {
  return source.replace(/<!--[\s\S]*?-->/g, '').replace(/\/\*[\s\S]*?\*\//g, '');
}

const sources = vueFiles(root).map((path) => ({
  path: path.slice(root.length + 1).replaceAll('\\', '/'),
  source: withoutComments(readFileSync(path, 'utf8')),
}));

describe('data tables on a phone', () => {
  // A prop the library ignores is worse than no prop: it reads as though the case is handled.
  it('asks for nothing PrimeVue 4 does not implement', () => {
    const offenders = sources
      .filter(({ source }) => /responsive-layout|responsiveLayout/.test(source))
      .map(({ path }) => path);

    expect(offenders, `these pass a PrimeVue 3 prop that PrimeVue 4 ignores:\n${offenders.join('\n')}`)
      .toEqual([]);
  });

  /**
   * CSS cannot read a table header, so a stacked cell gets its name from `data-label`. A column
   * without one becomes an unlabelled figure on a phone — a number with nothing saying what it is.
   */
  it('names every column, so a stacked cell still says what it is', () => {
    const offenders: string[] = [];
    for (const { path, source } of sources) {
      const columns = source.match(/<Column\b[\s\S]*?(?:\/>|>)/g) ?? [];
      const unlabelled = columns.filter((column) => !/data-label/.test(column)).length;
      if (unlabelled > 0) offenders.push(`${path}: ${unlabelled} of ${columns.length}`);
    }

    expect(offenders, `these columns lose their name when stacked:\n${offenders.join('\n')}`)
      .toEqual([]);
  });
});
