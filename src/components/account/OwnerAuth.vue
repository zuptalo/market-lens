<script setup lang="ts">
import { computed, ref } from 'vue';

const props = defineProps<{
  mode: 'setup' | 'email' | 'otp' | 'owner-password';
  capability?: string;
  email?: string;
  busy?: boolean;
  error?: string | null;
  message?: string | null;
}>();
const emit = defineEmits<{
  submit: [value: Record<string, string>];
  useOwnerPassword: [];
}>();

const displayName = ref('');
const enteredEmail = ref('');
const password = ref('');
const code = ref('');
const eodhdApiKey = ref('');
const smtpHost = ref('');
const smtpPort = ref('587');
const smtpFrom = ref('');
const smtpUsername = ref('');
const smtpPassword = ref('');
const heading = computed(() => ({
  setup: 'Create the owner account', email: 'Sign in', otp: 'Enter your passcode',
  'owner-password': 'Use owner password',
})[props.mode]);
const submitLabel = computed(() => ({
  setup: 'Create owner', email: 'Continue', otp: 'Verify passcode', 'owner-password': 'Sign in',
})[props.mode]);

function submit(): void {
  if (props.mode === 'setup') {
    emit('submit', {
      capability: props.capability ?? '', displayName: displayName.value, email: enteredEmail.value,
      password: password.value, eodhdApiKey: eodhdApiKey.value, smtpHost: smtpHost.value,
      smtpPort: smtpPort.value, smtpFrom: smtpFrom.value, smtpUsername: smtpUsername.value,
      smtpPassword: smtpPassword.value,
    });
    return;
  }
  if (props.mode === 'email') {
    emit('submit', { email: enteredEmail.value });
    return;
  }
  if (props.mode === 'otp') {
    emit('submit', { email: props.email ?? '', code: code.value });
    return;
  }
  emit('submit', { email: props.email ?? '', password: password.value });
}
</script>

<template>
  <form class="owner-auth" novalidate @submit.prevent="submit">
    <h1>{{ heading }}</h1>
    <p v-if="message" role="status">{{ message }}</p>
    <p v-if="error" role="alert">{{ error }}</p>

    <template v-if="mode === 'setup'">
      <label for="owner-display-name">Display name</label>
      <input id="owner-display-name" v-model="displayName" name="displayName" autocomplete="name" maxlength="120" required>
    </template>

    <template v-if="mode === 'setup' || mode === 'email'">
      <label for="owner-email">Email</label>
      <input id="owner-email" v-model="enteredEmail" name="email" type="email" inputmode="email" autocomplete="email" maxlength="320" required>
    </template>

    <template v-if="mode === 'setup' || mode === 'owner-password'">
      <label for="owner-password">Password</label>
      <input id="owner-password" v-model="password" name="password" type="password" :autocomplete="mode === 'owner-password' ? 'current-password' : 'new-password'" minlength="12" maxlength="1024" required>
    </template>

    <template v-if="mode === 'otp'">
      <label for="member-code">Six-digit passcode</label>
      <input id="member-code" v-model="code" name="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required>
    </template>

    <fieldset v-if="mode === 'setup'" class="owner-auth__integration">
      <legend>Market data</legend>
      <label for="eodhd-api-key">EODHD API key</label>
      <input id="eodhd-api-key" v-model="eodhdApiKey" name="eodhdApiKey" type="password" autocomplete="off" maxlength="1024" required>
    </fieldset>

    <fieldset v-if="mode === 'setup'" class="owner-auth__integration">
      <legend>Email delivery</legend>
      <label for="smtp-host">SMTP host</label>
      <input id="smtp-host" v-model="smtpHost" name="smtpHost" autocomplete="off" maxlength="253" required>
      <label for="smtp-port">SMTP port</label>
      <input id="smtp-port" v-model="smtpPort" name="smtpPort" inputmode="numeric" pattern="[0-9]{1,5}" maxlength="5" required>
      <label for="smtp-from">From email</label>
      <input id="smtp-from" v-model="smtpFrom" name="smtpFrom" type="email" autocomplete="email" maxlength="320" required>
      <label for="smtp-username">SMTP username (optional)</label>
      <input id="smtp-username" v-model="smtpUsername" name="smtpUsername" autocomplete="username" maxlength="320">
      <label for="smtp-password">SMTP password (optional)</label>
      <input id="smtp-password" v-model="smtpPassword" name="smtpPassword" type="password" autocomplete="new-password" maxlength="1024">
    </fieldset>

    <button type="submit" :disabled="busy">{{ busy ? 'Please wait…' : submitLabel }}</button>
    <button v-if="mode === 'otp'" type="button" data-owner-password :disabled="busy" @click="emit('useOwnerPassword')">
      Use owner password
    </button>
  </form>
</template>
