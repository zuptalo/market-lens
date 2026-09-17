<script setup lang="ts">
import { RouterLink } from 'vue-router';

/**
 * What the product could not determine.
 *
 * Every absence here is already stated honestly on the screen that owns it — an unvaluable holding,
 * an unevaluable limit. Individually honest and collectively invisible: somebody can read three
 * screens that each mention one missing figure without ever forming the thought that their picture
 * has holes in it.
 *
 * With nothing missing this renders nothing at all. A heading that always says "nothing missing"
 * teaches a reader to skip the region, and then they skip it on the day it says otherwise.
 */

export interface UnknownItem {
  label: string;
  detail: string;
  to: string;
  linkLabel: string;
}

const props = defineProps<{ items: UnknownItem[] }>();
</script>

<template>
  <section
    v-if="props.items.length > 0"
    class="unknown"
    aria-labelledby="unknown-heading"
    data-testid="unknown"
  >
    <h2 id="unknown-heading">What this could not tell you</h2>
    <ul class="unknown__list">
      <li v-for="item in props.items" :key="item.label">
        <span class="unknown__label">{{ item.label }}</span>
        <span class="unknown__detail">{{ item.detail }}</span>
        <RouterLink :to="item.to">{{ item.linkLabel }}</RouterLink>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.unknown__list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 1rem;
}

.unknown__label {
  display: block;
  font-weight: 600;
}

.unknown__detail {
  display: block;
  color: var(--p-text-muted-color);
  max-width: 62ch;
}
</style>
