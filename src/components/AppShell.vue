<script setup lang="ts">
import Button from 'primevue/button';
import Tag from 'primevue/tag';
import { useTheme } from '@/composables/useTheme';
import { buildVersion } from '@/utils/version';

const { preference, cycleTheme } = useTheme();
</script>

<template>
  <div class="app-shell">
    <header class="app-header">
      <RouterLink class="brand" to="/" aria-label="Market Lens home">
        <span class="brand-mark" aria-hidden="true">ML</span>
        <span>Market Lens</span>
      </RouterLink>
      <nav class="primary-nav" aria-label="Primary navigation">
        <RouterLink to="/">Overview</RouterLink>
        <RouterLink to="/markets">Market data</RouterLink>
      </nav>
      <div class="app-actions">
        <Tag
          class="app-version"
          :value="buildVersion"
          severity="secondary"
          :aria-label="`Market Lens version ${buildVersion}`"
        />
        <Button
          severity="secondary"
          variant="text"
          :label="`Theme: ${preference}`"
          aria-label="Change color theme"
          @click="cycleTheme"
        />
      </div>
    </header>
    <main class="app-content"><slot /></main>
  </div>
</template>
