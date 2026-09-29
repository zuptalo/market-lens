<script setup lang="ts">
import { computed } from 'vue';
import Message from 'primevue/message';
import type { FallbackState } from '@/types/marketData';
import { formatDate } from '@/utils/datetime';

/**
 * Says, on every screen, that prices are coming from the fallback provider (feature 030).
 *
 * A fallback nobody can see hides the very problem it covers for, so while it lasts this is the
 * standing signal — there are no reminders. It says since when and how many, and what the product
 * holds back while it lasts, so a figure on any screen can be read for what it is. Text, never
 * colour alone; and nothing about what anybody should do with their money.
 */
const props = defineProps<{ state: FallbackState | null }>();

const instruments = computed(() => {
  const count = props.state?.instruments ?? 0;
  return `${count} ${count === 1 ? 'instrument' : 'instruments'}`;
});
</script>

<template>
  <div v-if="state?.active" class="fallback-banner" role="status" aria-live="polite">
    <Message severity="warn" :closable="false">
      <p v-if="state.pendingReconciliation" class="fallback-text">
        The primary provider is delivering again. Prices for {{ instruments }} from
        {{ formatDate(state.since) }} still come from the fallback provider until the owner
        reconciles them. Paper orders wait for the primary provider's prices.
      </p>
      <p v-else class="fallback-text">
        Prices for {{ instruments }} since {{ formatDate(state.since) }} come from the fallback
        provider, because the primary provider refused access. Paper orders wait rather than fill,
        and backtests read only the primary provider's prices.
      </p>
    </Message>
  </div>
</template>

<style scoped>
.fallback-banner {
  padding: 0.5rem var(--app-gutter, 1rem) 0;
}

.fallback-text {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
