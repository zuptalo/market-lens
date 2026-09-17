<script setup lang="ts">
import Message from 'primevue/message';
import { RouterLink } from 'vue-router';

/**
 * What moved, and when.
 *
 * The reason to open this screen on a day when nothing needs anybody. Dates and counts only: a
 * value here would be a second copy of a figure another screen owns.
 */

export interface ChangeItem {
  label: string;
  detail: string;
  to: string;
  linkLabel: string;
}

const props = withDefaults(defineProps<{
  items: ChangeItem[];
  unreadable?: string[];
}>(), { unreadable: () => [] });
</script>

<template>
  <section
    v-if="props.items.length > 0 || props.unreadable.length > 0"
    class="changed"
    aria-labelledby="changed-heading"
    data-testid="changed"
  >
    <h2 id="changed-heading">What changed</h2>

    <Message
      v-for="source in props.unreadable"
      :key="source"
      severity="warn"
      :closable="false"
    >
      {{ source }} could not be read.
    </Message>

    <ul class="changed__list">
      <li v-for="item in props.items" :key="item.label">
        <span class="changed__label">{{ item.label }}</span>
        <span class="changed__detail">{{ item.detail }}</span>
        <RouterLink :to="item.to">{{ item.linkLabel }}</RouterLink>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.changed__list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 1rem;
}

.changed__label {
  display: block;
  font-weight: 600;
}

.changed__detail {
  display: block;
  color: var(--p-text-muted-color);
  max-width: 62ch;
}
</style>
