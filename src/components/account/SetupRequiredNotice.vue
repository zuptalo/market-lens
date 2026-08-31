<script setup lang="ts">
import { ref } from 'vue';
import Button from 'primevue/button';

// The command is the product's own, so it is the same however this installation is run.
const SETUP_COMMAND = 'market-lens auth setup-link';

const copied = ref(false);

async function copyCommand(): Promise<void> {
  try {
    await navigator.clipboard.writeText(SETUP_COMMAND);
    copied.value = true;
  } catch {
    // The command is printed on screen regardless; only the shortcut is unavailable.
    copied.value = false;
  }
}
</script>

<template>
  <section class="setup-notice">
    <h1>This installation has not been set up yet</h1>
    <p>
      Market Lens has no accounts. The first owner is created from a one-time link that the
      server prints for whoever is running it.
    </p>
    <p>
      It works this way because an open setup page would let anyone who reached this address
      first claim ownership of the installation. Asking the server for the link is what
      proves the person completing setup is the one running it.
    </p>
    <h2>Get the setup link</h2>
    <p>Run this wherever Market Lens itself is running, then open the link it prints:</p>
    <div class="setup-notice__command">
      <code>{{ SETUP_COMMAND }}</code>
      <Button
        type="button" data-copy-setup-command severity="secondary" size="small"
        :label="copied ? 'Copied' : 'Copy command'" @click="copyCommand" />
    </div>
    <p class="setup-notice__hint">
      If Market Lens runs inside a container, run it inside that container. The link is
      printed once, is never written to the logs, and expires after fifteen minutes; run the
      command again for a fresh one.
    </p>
  </section>
</template>

<style scoped>
.setup-notice { display: grid; gap: .75rem; max-width: 44rem; }
.setup-notice h2 { margin: .5rem 0 0; font-size: 1rem; }
.setup-notice__command {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: .5rem;
  padding: .75rem;
  border-radius: .65rem;
  background: color-mix(in srgb, currentColor 6%, transparent);
}
.setup-notice__command code { font-size: .95rem; overflow-wrap: anywhere; }
.setup-notice__hint { font-size: .9rem; }
</style>
