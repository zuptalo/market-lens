<script setup lang="ts">
import { computed } from 'vue';
import Button from 'primevue/button';

const props = withDefaults(defineProps<{
  loaded: number;
  total: number | null;
  hasMore: boolean;
  loading?: boolean;
  error?: string;
  /** How many rows arrived in the last page, so the announcement can say. */
  lastArrival?: number;
}>(), { loading: false, error: '', lastArrival: 0 });

const emit = defineEmits<{ (event: 'more'): void }>();

const counted = (value: number) => new Intl.NumberFormat('en-GB').format(value);

/**
 * Where the reader is. "10 of 342" is the difference between having seen enough and having
 * seen everything — a list that just stops says nothing about which.
 */
const position = computed(() => {
  if (props.total === null) return `${counted(props.loaded)} shown`;
  return `${counted(props.loaded)} of ${counted(props.total)}`;
});

const atEnd = computed(() => !props.hasMore && !props.loading);

/**
 * What assistive technology hears after a page arrives: how many came and where that leaves
 * the reader. Polite, because arriving rows are not urgent and interrupting a reader who is
 * moving through a list is worse than telling them a moment later.
 */
const announcement = computed(() => {
  if (props.error) return props.error;
  if (props.loading) return 'Loading more instruments.';
  if (atEnd.value) {
    return props.total === null
      ? `End of the list. ${counted(props.loaded)} shown.`
      : `End of the list. All ${counted(props.total)} shown.`;
  }
  if (props.lastArrival > 0) return `${counted(props.lastArrival)} more instruments. ${position.value}.`;
  return position.value;
});
</script>

<template>
  <div class="listing-progress">
    <p class="listing-progress__position" data-testid="listing-progress">
      <span>{{ position }}</span>
      <span v-if="atEnd && props.total !== null" class="listing-progress__end">End of the list</span>
    </p>

    <p
      class="listing-progress__announcement"
      data-testid="listing-progress-announcement"
      aria-live="polite"
      aria-atomic="true"
    >
      {{ announcement }}
    </p>

    <p v-if="props.error" class="listing-progress__error" role="alert">{{ props.error }}</p>

    <!--
      Kept even while scrolling loads pages by itself. This is the keyboard and screen-reader
      path: an endlessly growing list with no focusable end is a well-known way to strand
      those readers, and hiding the control once the observer works recreates it.
    -->
    <!--
      Deliberately not PrimeVue's `loading`, which disables the button: a disabled element
      loses focus, so activating this by keyboard would drop the reader's place — the very
      thing arriving rows must never do. Busy is announced instead, and the single-flight
      guard in the view makes a second activation harmless.
    -->
    <Button
      v-if="props.hasMore"
      type="button"
      severity="secondary"
      size="small"
      :aria-busy="props.loading"
      :label="props.error ? 'Try again' : 'Load more'"
      data-testid="load-more"
      @click="emit('more')"
    />
  </div>
</template>

<style scoped>
.listing-progress {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  padding: 0.75rem 0;
}

.listing-progress__position {
  color: var(--p-text-muted-color);
  display: flex;
  flex-wrap: wrap;
  font-size: 0.875rem;
  gap: 0.5rem;
  margin: 0;
}

.listing-progress__end {
  color: var(--p-text-color);
}

.listing-progress__error {
  color: var(--p-red-400, crimson);
  font-size: 0.875rem;
  margin: 0;
  width: 100%;
}

/* Announced, not shown: the position above already says it on screen. */
.listing-progress__announcement {
  clip: rect(0 0 0 0);
  clip-path: inset(50%);
  height: 1px;
  margin: -1px;
  overflow: hidden;
  position: absolute;
  white-space: nowrap;
  width: 1px;
}
</style>
