<script setup lang="ts">
import ProgressSpinner from 'primevue/progressspinner';

/**
 * A first load, shown where the content will be.
 *
 * A table's own loading overlay covers the table — which, before any rows exist, is only as tall
 * as its header row. So the spinner appeared *on the column headings*, pointing at the one part
 * of the screen that was already finished, while the empty space below it said nothing.
 *
 * This reserves the space the content is about to occupy and puts the spinner in it. It is for a
 * first load only: once rows exist, dimming them in place is right, because the reader can still
 * see what they had and where the new rows will land.
 */
withDefaults(defineProps<{ label: string; rows?: number }>(), { rows: 6 });
</script>

<template>
  <div
    class="loading-block"
    role="status"
    aria-live="polite"
    :style="{ minHeight: `${rows * 3.25}rem` }"
    data-testid="loading-block"
  >
    <ProgressSpinner
      class="loading-block__spinner"
      stroke-width="4"
      aria-hidden="true"
      style="width: 2.5rem; height: 2.5rem"
    />
    <p class="loading-block__label">{{ label }}</p>
  </div>
</template>

<style scoped>
.loading-block {
  display: flex;
  flex-direction: column;
  align-items: center;
  /*
    Near the top, not centred. The block reserves the height the rows are about to take, and a
    spinner centred in that reserved space can sit below the fold — which puts the one moving
    thing on the screen where the reader cannot see it. It belongs where the first rows will land.
  */
  justify-content: flex-start;
  gap: 0.75rem;
  border: 1px dashed var(--p-content-border-color);
  border-radius: var(--p-content-border-radius, 6px);
  padding: 3rem 1rem 2rem;
}

/*
  PrimeVue's spinner cycles its stroke through four colours by default, one of which is red — on
  a screen whose error state is also red, a loading indicator must not spend a quarter of its time
  looking like a failure. One colour, the theme's own.
*/
.loading-block__spinner :deep(.p-progressspinner-circle) {
  animation: p-progressspinner-dash 1.5s ease-in-out infinite;
  stroke: var(--p-primary-color);
}

.loading-block__label {
  margin: 0;
  color: var(--p-text-muted-color);
}
</style>
