<script setup lang="ts">
import Button from 'primevue/button';
import { useTheme } from '@/composables/useTheme';

const { preference, cycleTheme } = useTheme();
</script>

<template>
  <div class="app-shell">
    <header class="app-header">
      <RouterLink class="brand" to="/" aria-label="Market Lens home">
        <span class="brand-mark" aria-hidden="true">ML</span>
        <span>Market Lens</span>
      </RouterLink>
      <Button
        severity="secondary"
        variant="text"
        :label="`Theme: ${preference}`"
        aria-label="Change color theme"
        @click="cycleTheme"
      />
    </header>
    <main class="app-content"><slot /></main>
  </div>
</template>
