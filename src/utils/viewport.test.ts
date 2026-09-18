import { beforeEach, describe, expect, it, vi } from 'vitest';
import { lockZoomWhenInstalled } from './viewport';

function viewportMeta(): HTMLMetaElement {
  return document.querySelector('meta[name="viewport"]') as HTMLMetaElement;
}

function installedAs(mode: 'standalone' | 'browser'): void {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query.includes('standalone') && mode === 'standalone',
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
}

describe('zoom in the installed app', () => {
  beforeEach(() => {
    document.head.innerHTML = '<meta name="viewport" content="width=device-width, initial-scale=1.0">';
  });

  /**
   * Installed to a home screen, this should feel like an application rather than a page. A
   * double-tap that zooms instead of activating, or a two-finger scroll that scales the interface,
   * is the tell that it is a website in a costume.
   */
  it('stops the interface being scaled once installed', () => {
    installedAs('standalone');
    lockZoomWhenInstalled();
    expect(viewportMeta().content).toContain('user-scalable=no');
    expect(viewportMeta().content).toContain('maximum-scale=1');
    expect(viewportMeta().content).toContain('width=device-width');
  });

  /**
   * In a browser tab it stays zoomable, deliberately. Taking pinch-zoom away from a page is taking
   * it away from somebody who needs it to read, and a tab is where that person is.
   */
  it('leaves a browser tab alone', () => {
    installedAs('browser');
    lockZoomWhenInstalled();
    expect(viewportMeta().content).not.toContain('user-scalable=no');
    expect(viewportMeta().content).not.toContain('maximum-scale');
  });

  it('copes with a document that has no viewport tag', () => {
    document.head.innerHTML = '';
    installedAs('standalone');
    expect(() => lockZoomWhenInstalled()).not.toThrow();
  });

  // Running twice must not append a second copy of the rules.
  it('is safe to apply more than once', () => {
    installedAs('standalone');
    lockZoomWhenInstalled();
    lockZoomWhenInstalled();
    expect(viewportMeta().content.match(/user-scalable=no/g)).toHaveLength(1);
  });
});
