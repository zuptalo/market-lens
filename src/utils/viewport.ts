/**
 * How the page responds to being pinched, which depends on where it is running.
 *
 * Installed to a home screen this should behave like an application: a double-tap activates a
 * control rather than magnifying it, and two fingers scroll rather than scale. In a browser tab it
 * stays zoomable, deliberately — removing pinch-zoom from a web page removes it from the person
 * who needed it to read the page, and a tab is where that person is.
 *
 * The distinction is `display-mode: standalone`, which is true exactly when the app was installed.
 */

const LOCKED = 'width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no, viewport-fit=cover';

export function lockZoomWhenInstalled(): void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
  const installed = window.matchMedia('(display-mode: standalone)').matches
    // iOS answers the installed question its own way, and has since long before display-mode.
    || (window.navigator as { standalone?: boolean }).standalone === true;
  if (!installed) return;

  const meta = document.querySelector('meta[name="viewport"]');
  if (!(meta instanceof HTMLMetaElement)) return;
  meta.setAttribute('content', LOCKED);
}
