<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import OwnerAuth from '@/components/account/OwnerAuth.vue';
import { useAuth } from '@/composables/useAuth';
import SetupRequiredNotice from '@/components/account/SetupRequiredNotice.vue';

const auth = useAuth();
const router = useRouter();
const capability = ref('');
const setupRequired = ref<boolean | null>(null);
const busy = ref(false);
const error = ref<string | null>(null);

onMounted(async () => {
  capability.value = window.location.hash.slice(1);
  if (window.location.hash) window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`);
  try {
    setupRequired.value = (await auth.setupStatus()).setupRequired;
  } catch {
    error.value = 'Owner setup is temporarily unavailable.';
  }
});

async function setup(value: Record<string, string>): Promise<void> {
  busy.value = true;
  error.value = null;
  try {
    await auth.completeOwnerSetup({
      capability: value.capability, displayName: value.displayName, email: value.email, password: value.password,
      eodhdApiKey: value.eodhdApiKey,
      smtp: {
        host: value.smtpHost, port: Number(value.smtpPort), from: value.smtpFrom,
        username: value.smtpUsername, password: value.smtpPassword,
      },
    });
    capability.value = '';
    await router.replace('/');
  } catch (failure) {
    error.value = failure instanceof Error ? failure.message : 'Owner setup failed.';
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section class="auth-view">
    <p v-if="setupRequired === false" role="status">Owner setup is permanently closed.</p>
    <SetupRequiredNotice v-else-if="setupRequired && !capability" />
    <OwnerAuth
      v-else-if="capability"
      mode="setup"
      :capability="capability"
      :busy="busy"
      :error="error"
      @submit="setup"
    />
    <p v-else-if="error" role="alert">{{ error }}</p>
  </section>
</template>
