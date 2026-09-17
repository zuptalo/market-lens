<script setup lang="ts">
import Message from 'primevue/message';
import { RouterLink } from 'vue-router';

/**
 * What needs a person.
 *
 * This product has deliberately accumulated decisions only a person can make — findings the nightly
 * pass refuses to settle, limits it refuses to advise on — and scattered them across three screens.
 * This is the one place that says they exist.
 *
 * It says what is waiting and where to go. It does not say what to do about any of it: the screens
 * that own these items do not either, and the Overview is not the place that starts.
 */

export interface WaitingItem {
  /** What is waiting, already counted — "2 findings awaiting a decision". */
  label: string;
  /** Why it is waiting, in a sentence somebody can act on. */
  detail: string;
  to: string;
  linkLabel: string;
}

const props = withDefaults(defineProps<{
  items: WaitingItem[];
  /** Sources that could not be read. Named, never counted as zero. */
  unreadable?: string[];
}>(), { unreadable: () => [] });
</script>

<template>
  <section class="waiting" aria-labelledby="waiting-heading" data-testid="waiting">
    <h2 id="waiting-heading">Waiting for you</h2>

    <!-- A failed read is not an all-clear. The whole value of this screen is "nothing needs you"
         being trustworthy, and a source that did not load must not look like one that was empty. -->
    <Message
      v-for="source in props.unreadable"
      :key="source"
      severity="warn"
      :closable="false"
      class="waiting__unreadable"
      data-testid="waiting-unreadable"
    >
      {{ source }} could not be read, so anything waiting there is not counted here.
    </Message>

    <Message
      v-if="props.items.length === 0 && props.unreadable.length === 0"
      severity="success"
      :closable="false"
      data-testid="waiting-clear"
    >
      Nothing needs you.
    </Message>

    <ul v-else-if="props.items.length > 0" class="waiting__list">
      <li v-for="item in props.items" :key="item.label">
        <span class="waiting__label">{{ item.label }}</span>
        <span class="waiting__detail">{{ item.detail }}</span>
        <RouterLink :to="item.to">{{ item.linkLabel }}</RouterLink>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.waiting__list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 1rem;
}

.waiting__label {
  display: block;
  font-weight: 600;
}

.waiting__detail {
  display: block;
  color: var(--p-text-muted-color);
  max-width: 62ch;
}

.waiting__unreadable {
  margin-bottom: 0.75rem;
}
</style>
