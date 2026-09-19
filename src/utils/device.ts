/**
 * The name of a device, read out of the user-agent string the browser sent when it signed in.
 *
 * The session list used to print that string whole. On a phone one signed-in device filled
 * most of the screen with `Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X)
 * AppleWebKit/605.1.15 (KHTML, like Gecko) Version/27.0 Mobile/15E148 Safari/604.1`, and the
 * two facts a person actually audits with — which browser, which kind of machine — were buried
 * in boilerplate that has been lying about itself since 1994.
 *
 * This is a display name and nothing else. Nothing is authorised by it, the client chose it,
 * and it is recognition rather than identification.
 */

const UNKNOWN = 'Unknown device';

/**
 * Most specific first. Every Chromium browser also says `Chrome`, Chrome also says `Safari`,
 * and Edge says both — so the order of this list is the whole of the logic.
 */
const BROWSERS: ReadonlyArray<readonly [RegExp, string]> = [
  [/\bEdg(?:e|A|iOS)?\//, 'Edge'],
  [/\b(?:OPR|Opera)\//, 'Opera'],
  [/\bSamsungBrowser\//, 'Samsung Internet'],
  [/\b(?:CriOS|Chrome|Chromium)\//, 'Chrome'],
  [/\b(?:FxiOS|Firefox)\//, 'Firefox'],
  [/\bSafari\//, 'Safari'],
];

/** iPhone and iPad before the Mac they both claim to be like, Android before the Linux it is. */
const PLATFORMS: ReadonlyArray<readonly [RegExp, string]> = [
  [/\biPhone\b/, 'iPhone'],
  [/\biPad\b/, 'iPad'],
  [/\biPod\b/, 'iPod'],
  [/\bAndroid\b/, 'Android'],
  [/\bCrOS\b/, 'ChromeOS'],
  [/\b(?:Macintosh|Mac OS X)\b/, 'macOS'],
  [/\bWindows\b/, 'Windows'],
  [/\b(?:Linux|X11)\b/, 'Linux'],
];

/**
 * A user-agent string always carries at least one `product/version` token. A label that has none is
 * not one — it is a name somebody or something else chose, and reading it as a user agent finds
 * the word Linux inside "Chrome on Linux" and answers with the less specific half.
 */
const PRODUCT_VERSION = /[A-Za-z][A-Za-z0-9._-]*\/[0-9]/;

function match(userAgent: string, table: ReadonlyArray<readonly [RegExp, string]>): string | null {
  for (const [pattern, name] of table) if (pattern.test(userAgent)) return name;
  return null;
}

export function describeDevice(deviceLabel: string | null | undefined): string {
  const userAgent = (deviceLabel ?? '').trim();
  if (userAgent === '') return UNKNOWN;
  if (!PRODUCT_VERSION.test(userAgent)) return userAgent;
  const browser = match(userAgent, BROWSERS);
  const platform = match(userAgent, PLATFORMS);
  if (browser && platform) return `${browser} on ${platform}`;
  // Half a name is still a name. Nothing recognisable at all is left exactly as it arrived:
  // on a security screen the session that does not look like a browser is the one to read.
  return browser ?? platform ?? userAgent;
}
