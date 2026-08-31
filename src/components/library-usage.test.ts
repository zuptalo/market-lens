import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

// Feature 012. PrimeVue is the component library this project picked, so a bare control means
// somebody rebuilt something the library already provides - and then had to restyle its focus,
// disabled, and invalid states by hand, which is how the two forms on the account screen came
// to look different and how every account alert ended up unreadable on dark.
const RAW_CONTROL = /<(input|button|select|textarea)\b[^>]*>/g;

// A secret field is the one place a raw input is correct. Both PrimeVue Password and InputText
// bind their value as a DOM *attribute*, so a typed API key or mail password is serialized into
// the markup and captured by anything that snapshots the DOM. A plain input sets the value as a
// property instead. These carry the library's classes, so they look identical to every other
// field, and OwnerAuth.test.ts enforces that no secret reaches rendered HTML.
// Whole-file exemptions, each stating why the library has no equivalent. Expected to stay
// empty: a per-tag exception (below) is always preferable to exempting a file wholesale.
const ALLOWED: Record<string, string> = {};

// Comments discuss markup as prose; only what is actually rendered counts.
function withoutComments(source: string): string {
  return source
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|\s)\/\/.*$/gm, '');
}

function permitted(tag: string): boolean {
  return tag.startsWith('<input') && /type="password"/.test(tag);
}

function vueFiles(directory: string): string[] {
  return readdirSync(directory).flatMap((entry) => {
    const path = join(directory, entry);
    if (statSync(path).isDirectory()) return vueFiles(path);
    return path.endsWith('.vue') ? [path] : [];
  });
}

describe('component library usage', () => {
  it('builds no control the library already provides', () => {
    const root = join(process.cwd(), 'src');
    const offenders = vueFiles(root)
      .filter((path) => {
        const relative = path.slice(root.length + 1).replaceAll('\\', '/');
        if (ALLOWED[relative]) return false;
        const tags = withoutComments(readFileSync(path, 'utf8')).match(RAW_CONTROL) ?? [];
        return tags.some((tag) => !permitted(tag));
      })
      .map((path) => path.slice(root.length + 1));

    expect(offenders, `these render raw controls instead of PrimeVue components:\n${offenders.join('\n')}`)
      .toEqual([]);
  });

  it('does not restyle library controls in the project stylesheet', () => {
    // Component <style> blocks count too. A scoped block is where the second border, radius
    // and padding lived that drew a box inside the Fieldset already drawing one.
    const scoped = vueFiles(join(process.cwd(), 'src'))
      .flatMap((path) => readFileSync(path, 'utf8').match(/<style[^>]*>([\s\S]*?)<\/style>/g) ?? []);
    const stylesheet = [readFileSync(join(process.cwd(), 'src/styles/main.css'), 'utf8'), ...scoped]
      .join('\n')
      .replace(/\/\*[\s\S]*?\*\//g, '');

    // Layout is still ours to decide - a button may be told to fill its row on a phone. What
    // the theme owns is chrome: colour, border, radius, shadow, and type. A descendant
    // selector restyles just as effectively as a bare one, which is how five hand-rolled
    // button rules survived the first pass and left two shades of teal on one screen.
    const controlSelector = /(^|[\s,>])(input|button|select|textarea)([\s:[,{]|$)/;
    const chrome = /(^|[\s;{])(background|color|border|outline|box-shadow|font|cursor)[-a-z]*\s*:/;

    const offenders = stylesheet
      .split('}')
      .map((rule) => rule.trim())
      .filter((rule) => rule.includes('{'))
      .filter((rule) => {
        const [selector, body = ''] = rule.split('{');
        return controlSelector.test(selector) && chrome.test(body);
      })
      .map((rule) => rule.split('{')[0].trim());

    expect(offenders, `main.css restyles controls the theme owns:\n${offenders.join('\n')}`).toEqual([]);
  });
});
