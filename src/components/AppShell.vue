<script setup lang="ts">
import Button from 'primevue/button';
import Tag from 'primevue/tag';
import { useTheme } from '@/composables/useTheme';
import { buildVersion } from '@/utils/version';
import { useAuth } from '@/composables/useAuth';

const { preference, cycleTheme } = useTheme();
const auth = useAuth();
</script>

<template>
  <div class="app-shell">
    <header class="app-header">
      <RouterLink class="brand" to="/" aria-label="Market Lens home">
        <span class="brand-mark" aria-hidden="true">ML</span>
        <span>Market Lens</span>
      </RouterLink>
      <nav class="primary-nav" aria-label="Primary navigation">
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/">Overview</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/markets">Market data</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/signals">Signals</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/operations">Operations</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/account">Account</RouterLink>
        <RouterLink v-else to="/login">Sign in</RouterLink>
      </nav>
      <div class="app-actions">
        <Tag
          class="app-version"
          :value="buildVersion"
          severity="secondary"
          :aria-label="`Market Lens version ${buildVersion}`"
        />
        <Button
          class="theme-toggle"
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
