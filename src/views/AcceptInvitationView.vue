<script setup lang="ts">
import { onMounted, ref } from 'vue';
import InputText from 'primevue/inputtext';
import Button from 'primevue/button';
import Message from 'primevue/message';
import { useRouter } from 'vue-router';
import { useAuth } from '@/composables/useAuth';

const auth = useAuth();
const router = useRouter();
const email = ref('');
const displayName = ref('');
const busy = ref(false);
const error = ref<string | null>(null);
const capability = ref('');

// The capability arrives in the URL fragment so it never reaches the server in a request line.
// It is read into memory once and removed, so it does not linger in history or a copied link.
onMounted(() => {
  capability.value = window.location.hash.replace(/^#/, '');
  if (capability.value) {
    window.history.replaceState(null, '', window.location.pathname + window.location.search);
  }
});

async function submit(): Promise<void> {
  if (!capability.value) {
    error.value = 'This invitation is invalid or unavailable. Ask the owner to send a new one.';
    return;
  }
  busy.value = true;
  error.value = null;
  try {
    await auth.acceptInvitation({
      capability: capability.value, email: email.value, displayName: displayName.value,
    });
    await router.replace('/');
  } catch (failure) {
    error.value = failure instanceof Error
      ? 'This invitation is invalid or unavailable. Ask the owner to send a new one.'
      : 'This invitation is invalid or unavailable. Ask the owner to send a new one.';
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section class="auth-view">
    <form class="owner-auth" novalidate @submit.prevent="submit">
      <h1>Accept your invitation</h1>
      <p class="accept-invitation__hint">
        Confirm your details to join. You will sign in with a six-digit code sent to your email,
        so there is nothing to remember.
      </p>
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

      <label for="invitation-display-name">Display name</label>
      <InputText
        id="invitation-display-name"
        v-model="displayName"
        name="displayName"
        autocomplete="name"
        maxlength="120"
        required
        fluid
      />

      <label for="invitation-email">Email</label>
      <InputText
        id="invitation-email"
        v-model="email"
        name="email"
        type="email"
        inputmode="email"
        autocomplete="email"
        maxlength="320"
        required
        fluid
      />

      <Button type="submit" :disabled="busy" :label="busy ? 'Joining…' : 'Join Market Lens'" />
    </form>
  </section>
</template>
