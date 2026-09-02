<script setup lang="ts">
import type { ConnectionState } from '@/types/marketData';

/**
 * Whether the page is still hearing about changes.
 *
 * It is its own component because more than one screen needs to say so and only one of them
 * reports imports. Bundling the two put a hard-coded empty import list on the instrument screen,
 * which then told every reader that no import had ever run — on a page drawing a chart from
 * those very imports.
 */
defineProps<{ connectionState: ConnectionState }>();
</script>

<template>
  <span
    data-testid="connection-state"
    class="connection-state"
    :class="`connection-${connectionState}`"
  >Live updates: {{ connectionState }}</span>
</template>

<style scoped>
.connection-state {
  border-radius: 999px;
  font-size: 0.8125rem;
  padding: 0.2rem 0.6rem;
  white-space: nowrap;
  border: 1px solid var(--p-content-border-color);
  color: var(--p-text-muted-color);
}

.connection-connected {
  border-color: var(--p-primary-color);
  color: var(--p-primary-color);
}

.connection-stale,
.connection-offline {
  border-color: var(--p-orange-400, #d97706);
  color: var(--p-orange-400, #d97706);
}
</style>
