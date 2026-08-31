import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue';

/**
 * Whether the viewport is narrower than the tablet breakpoint.
 *
 * Layout is CSS's job almost everywhere in this project. This exists for the one case CSS
 * cannot handle: choosing *where in the DOM* a control lives, rather than how it looks.
 * Rendering the filters twice and hiding one would put two controls with the same accessible
 * name in the page, so the breakpoint has to be a value the template can branch on.
 *
 * `matchMedia` is guarded because jsdom does not implement it and neither do some embedded
 * renderers. Its absence resolves to the roomy layout, which is the safe assumption: it shows
 * every control rather than hiding them behind a trigger that may not be reachable.
 */
export const COMPACT_BREAKPOINT = '(max-width: 767px)';

export function useCompactViewport(breakpoint: string = COMPACT_BREAKPOINT): Ref<boolean> {
  const compact = ref(false);
  let list: MediaQueryList | undefined;
  const update = (event: MediaQueryList | MediaQueryListEvent) => {
    compact.value = event.matches;
  };

  onMounted(() => {
    if (typeof window.matchMedia !== 'function') return;
    list = window.matchMedia(breakpoint);
    compact.value = list.matches;
    list.addEventListener('change', update);
  });

  onBeforeUnmount(() => {
    list?.removeEventListener('change', update);
  });

  return compact;
}
