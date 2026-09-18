<script setup lang="ts">
import { ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import Button from 'primevue/button';
import Drawer from 'primevue/drawer';
import Tag from 'primevue/tag';
import { useTheme } from '@/composables/useTheme';
import { buildVersion } from '@/utils/version';
import { useAuth } from '@/composables/useAuth';

/**
 * The shell, and the one place this product decides how navigation behaves on a phone.
 *
 * Nine destinations do not fit one row at 390 CSS pixels. Letting them wrap took 253 of 800 — a
 * third of the screen spent before the page began — so below the tablet breakpoint the
 * destinations, the build number and the theme control move into a drawer, and the bar stays one
 * compact row of brand and menu.
 *
 * Which of the two is showing is decided entirely in CSS. Deciding it in JavaScript — matchMedia,
 * then re-render — left one frame after a resize or a rotation where the wide row was still in a
 * narrow window, and the page scrolled sideways for that frame. The drawer's own contents exist
 * only while it is open, so nothing is ever announced twice.
 */

const { preference, cycleTheme } = useTheme();
const auth = useAuth();
const route = useRoute();

const menuOpen = ref(false);

/*
 * Arriving somewhere closes the menu. A drawer still covering the page you just asked for is the
 * most common way this pattern is got wrong.
 *
 * Both of these are needed. The watcher covers navigation the app starts itself; the click handler
 * covers tapping the destination you are already on, where the route never changes and the watcher
 * therefore never fires — which left the menu open with no way back but the close button.
 */
watch(() => route.fullPath, () => { menuOpen.value = false; });

function closeMenu(): void { menuOpen.value = false; }
</script>

<template>
  <div class="app-shell">
    <header class="app-header">
      <RouterLink class="brand" to="/" aria-label="Market Lens home">
        <span class="brand-mark" aria-hidden="true">ML</span>
        <span>Market Lens</span>
      </RouterLink>

      <nav class="primary-nav primary-nav--inline" aria-label="Primary navigation">
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/">Overview</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/markets">Market data</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/signals">Signals</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/portfolio">Portfolio</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/risk">Limits</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/intents">Intents</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/paper">Paper</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/backtests">Backtests</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/operations">Operations</RouterLink>
        <RouterLink v-if="auth.state.status === 'authenticated'" to="/account">Account</RouterLink>
        <RouterLink v-else to="/login">Sign in</RouterLink>
      </nav>

      <div class="app-actions app-actions--inline">
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

      <Button
        class="menu-toggle"
        severity="secondary"
        variant="text"
        label="Menu"
        aria-label="Open navigation menu"
        :aria-expanded="menuOpen"
        @click="menuOpen = true"
      />

      <!--
        The drawer builds its contents only while it is open, so at wide widths this second copy of
        the destinations does not exist at all — not in the document and not in the accessibility
        tree. The control that opens it is hidden above the breakpoint, so it cannot be opened there.
      -->
      <Drawer v-model:visible="menuOpen" class="app-drawer" position="right" header="Go to">
        <nav class="primary-nav primary-nav--stacked" aria-label="Primary navigation" @click="closeMenu">
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/">Overview</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/markets">Market data</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/signals">Signals</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/portfolio">Portfolio</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/risk">Limits</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/intents">Intents</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/paper">Paper</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/backtests">Backtests</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/operations">Operations</RouterLink>
          <RouterLink v-if="auth.state.status === 'authenticated'" to="/account">Account</RouterLink>
          <RouterLink v-else to="/login">Sign in</RouterLink>
        </nav>
        <div class="app-actions app-actions--stacked">
          <Button
            class="theme-toggle"
            severity="secondary"
            variant="text"
            :label="`Theme: ${preference}`"
            aria-label="Change color theme"
            @click="cycleTheme"
          />
          <Tag
            class="app-version"
            :value="buildVersion"
            severity="secondary"
            :aria-label="`Market Lens version ${buildVersion}`"
          />
        </div>
      </Drawer>
    </header>
    <main class="app-content"><slot /></main>
  </div>
</template>
