<script setup lang="ts">
// Secret fields are the one deliberate exception to using the library's inputs. Both Password
// and InputText bind the value as a DOM *attribute*, so a typed API key or mail password ends
// up serialized into the markup and would be captured by anything that snapshots the DOM. A
// plain input sets the value as a property instead, which is what keeps it out of rendered
// HTML - the property OwnerAuth.test.ts exists to protect. They carry the library's own
// classes so they are visually identical to every other field.
import { computed, ref } from 'vue';
import InputText from 'primevue/inputtext';
import Button from 'primevue/button';
import Message from 'primevue/message';
import Fieldset from 'primevue/fieldset';

const props = defineProps<{
  mode: 'setup' | 'email' | 'otp' | 'owner-password';
  capability?: string;
  email?: string;
  busy?: boolean;
  error?: string | null;
  message?: string | null;
  // Keyed by the wire field name the server reported, so each message lands on the input it
  // belongs to instead of a single alert above ten of them.
  fieldErrors?: Record<string, string> | null;
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
const errorCount = computed(() => Object.keys(props.fieldErrors ?? {}).length);
const summary = computed(() => {
  if (!props.error) return null;
  if (errorCount.value === 0) return props.error;
  const plural = errorCount.value === 1 ? '1 field needs' : `${errorCount.value} fields need`;
  return `${props.error} (${plural} attention.)`;
});
function fieldError(field: string): string | null {
  return props.fieldErrors?.[field] ?? null;
}
function describedBy(field: string): string | undefined {
  return fieldError(field) ? `${field}-error` : undefined;
}
function invalid(field: string): 'true' | undefined {
  return fieldError(field) ? 'true' : undefined;
}
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
    <Message v-if="message" role="status" severity="info" :closable="false">{{ message }}</Message>
    <Message v-if="summary" severity="error" :closable="false">{{ summary }}</Message>

    <template v-if="mode === 'setup'">
      <label for="owner-display-name">Display name</label>
      <InputText id="owner-display-name" v-model="displayName" name="displayName" autocomplete="name" maxlength="120" required fluid
                 :invalid="!!fieldError('display_name')" :aria-describedby="describedBy('display_name')" />
      <p v-if="fieldError('display_name')" id="display_name-error" class="owner-auth__field-error">{{ fieldError('display_name') }}</p>
    </template>

    <template v-if="mode === 'setup' || mode === 'email'">
      <label for="owner-email">Email</label>
      <InputText id="owner-email" v-model="enteredEmail" name="email" type="email" inputmode="email" autocomplete="email" maxlength="320" required fluid
                 :invalid="!!fieldError('email')" :aria-describedby="describedBy('email')" />
      <p v-if="fieldError('email')" id="email-error" class="owner-auth__field-error">{{ fieldError('email') }}</p>
    </template>

    <template v-if="mode === 'setup' || mode === 'owner-password'">
      <label for="owner-password">Password</label>
      <input
        id="owner-password" v-model="password" name="password" type="password" class="p-inputtext p-component p-inputtext-fluid"
        :autocomplete="mode === 'owner-password' ? 'current-password' : 'new-password'"
        minlength="12" maxlength="1024" required
        :aria-invalid="invalid('password')" :aria-describedby="describedBy('password')">
      <p v-if="fieldError('password')" id="password-error" class="owner-auth__field-error">{{ fieldError('password') }}</p>
    </template>

    <template v-if="mode === 'otp'">
      <label for="member-code">Six-digit passcode</label>
      <InputText id="member-code" v-model="code" name="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required fluid />
    </template>

    <Fieldset v-if="mode === 'setup'" class="owner-auth__integration" legend="Market data">
      <label for="eodhd-api-key">EODHD API key</label>
      <input
        id="eodhd-api-key" v-model="eodhdApiKey" name="eodhdApiKey" type="password" class="p-inputtext p-component p-inputtext-fluid"
        autocomplete="off" maxlength="1024" required
        :aria-invalid="invalid('eodhd_api_key')" :aria-describedby="describedBy('eodhd_api_key')">
      <p v-if="fieldError('eodhd_api_key')" id="eodhd_api_key-error" class="owner-auth__field-error">{{ fieldError('eodhd_api_key') }}</p>
    </Fieldset>

    <Fieldset v-if="mode === 'setup'" class="owner-auth__integration" legend="Email delivery">
      <label for="smtp-host">SMTP host</label>
      <InputText id="smtp-host" v-model="smtpHost" name="smtpHost" autocomplete="off" maxlength="253" required fluid
                 :invalid="!!fieldError('smtp_host')" :aria-describedby="describedBy('smtp_host')" />
      <p v-if="fieldError('smtp_host')" id="smtp_host-error" class="owner-auth__field-error">{{ fieldError('smtp_host') }}</p>
      <label for="smtp-port">SMTP port</label>
      <InputText id="smtp-port" v-model="smtpPort" name="smtpPort" inputmode="numeric" pattern="[0-9]{1,5}" maxlength="5" required fluid
                 :invalid="!!fieldError('smtp_port')" :aria-describedby="describedBy('smtp_port')" />
      <p v-if="fieldError('smtp_port')" id="smtp_port-error" class="owner-auth__field-error">{{ fieldError('smtp_port') }}</p>
      <label for="smtp-from">From email</label>
      <InputText id="smtp-from" v-model="smtpFrom" name="smtpFrom" type="email" autocomplete="email" maxlength="320" required fluid
                 :invalid="!!fieldError('smtp_from')" :aria-describedby="describedBy('smtp_from')" />
      <p v-if="fieldError('smtp_from')" id="smtp_from-error" class="owner-auth__field-error">{{ fieldError('smtp_from') }}</p>
      <label for="smtp-username">SMTP username (optional)</label>
      <InputText id="smtp-username" v-model="smtpUsername" name="smtpUsername" autocomplete="username" maxlength="320" fluid
                 :invalid="!!fieldError('smtp_username')" :aria-describedby="describedBy('smtp_username')" />
      <p v-if="fieldError('smtp_username')" id="smtp_username-error" class="owner-auth__field-error">{{ fieldError('smtp_username') }}</p>
      <label for="smtp-password">SMTP password (optional)</label>
      <input
        id="smtp-password" v-model="smtpPassword" name="smtpPassword" type="password" class="p-inputtext p-component p-inputtext-fluid"
        autocomplete="new-password" maxlength="1024"
        :aria-invalid="invalid('smtp_password')" :aria-describedby="describedBy('smtp_password')">
      <p v-if="fieldError('smtp_password')" id="smtp_password-error" class="owner-auth__field-error">{{ fieldError('smtp_password') }}</p>
    </Fieldset>

    <Button type="submit" :disabled="busy" :label="busy ? 'Please wait…' : submitLabel" />
    <Button
      v-if="mode === 'otp'" type="button" data-owner-password severity="secondary"
      label="Use owner password" :disabled="busy" @click="emit('useOwnerPassword')" />
  </form>
</template>
