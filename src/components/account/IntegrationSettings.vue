<script setup lang="ts">
// The two secret fields stay native inputs: the library's inputs bind their value as a DOM
// attribute, which would serialize a typed API key or mail password into the markup. See
// OwnerAuth.vue for the same exception and the test that enforces it.
import { computed, ref, watch } from 'vue';
import InputText from 'primevue/inputtext';
import Button from 'primevue/button';
import Message from 'primevue/message';
import Panel from 'primevue/panel';
import Fieldset from 'primevue/fieldset';
import type { IntegrationSettingsView, IntegrationUpdateInput } from '@/types/auth';

const props = defineProps<{
  settings: IntegrationSettingsView | null;
  busy?: boolean;
  error?: string | null;
  message?: string | null;
  fieldErrors?: Record<string, string> | null;
  // Each integration's own outcome from the last check or save.
  results?: Record<string, string> | null;
}>();
const emit = defineEmits<{ save: [value: IntegrationUpdateInput]; verify: [value: IntegrationUpdateInput] }>();

const host = ref('');
const port = ref('587');
const from = ref('');
const username = ref('');
// Secrets are never returned, so these start empty and stay empty unless deliberately typed.
const password = ref('');
const eodhdKey = ref('');

watch(() => props.settings, (settings) => {
  if (!settings) return;
  host.value = settings.smtp.host;
  port.value = String(settings.smtp.port || 587);
  from.value = settings.smtp.from;
  username.value = settings.smtp.username;
}, { immediate: true });

const passwordHint = computed(() => {
  if (!props.settings?.smtp.passwordConfigured) return 'No password is saved. Leave blank to connect without authentication.';
  return 'A password is saved. Leave blank to keep it, or type a new one to replace it.';
});
const validatedHint = computed(() => {
  if (!props.settings?.eodhd.configured) return 'No API key is saved.';
  const validated = props.settings.eodhd.validatedAt;
  return validated ? `A key is saved, last checked ${validated}.` : 'A key is saved.';
});

// An untouched secret is omitted rather than sent blank: omitting the password keeps the
// stored one, and omitting the provider key leaves it alone entirely.
function build(): IntegrationUpdateInput {
  submittedEODHD.value = eodhdKey.value !== '';
  const update: IntegrationUpdateInput = {
    smtp: {
      host: host.value, port: Number(port.value), from: from.value, username: username.value,
      ...(password.value === '' ? {} : { password: password.value }),
    },
  };
  if (eodhdKey.value !== '') update.eodhd = { apiKey: eodhdKey.value };
  return update;
}

// Every integration reports separately, because one half of a configuration working tells the
// owner nothing about the other. "not_checked" is shown as a warning rather than a success:
// a value of the wrong shape stops every network call, so nothing was contacted at all.
const SECTION_STATUS: Record<string, Record<string, { severity: string; text: string }>> = {
  eodhd: {
    verified: { severity: 'success', text: 'EODHD accepted this API key.' },
    failed: { severity: 'error', text: 'EODHD did not accept this API key. See the message on the field above.' },
    not_checked: { severity: 'warn', text: 'The API key was not checked, because something else on this form needs fixing first.' },
    not_submitted: { severity: 'info', text: 'Not checked: the saved key is unchanged. Enter a new key to check it.' },
  },
  smtp: {
    verified: { severity: 'success', text: 'The mail server accepted this configuration.' },
    failed: { severity: 'error', text: 'The mail server did not accept this configuration. See the message on the field above.' },
    not_checked: { severity: 'warn', text: 'The mail settings were not checked, because something else on this form needs fixing first.' },
  },
};

// A section that was never submitted is a different thing from one the server skipped. Saying
// "something else needs fixing" when the field was simply left blank sends somebody hunting
// for a problem that does not exist.
const submittedEODHD = ref(false);

function sectionStatus(section: 'eodhd' | 'smtp'): { severity: string; text: string } | null {
  let outcome = props.results?.[section];
  if (!outcome) return null;
  if (section === 'eodhd' && outcome === 'not_checked' && !submittedEODHD.value) outcome = 'not_submitted';
  return SECTION_STATUS[section][outcome] ?? null;
}

function fieldError(field: string): string | null {
  return props.fieldErrors?.[field] ?? null;
}
function describedBy(field: string): string | undefined {
  return fieldError(field) ? `integration-${field}-error` : undefined;
}
function invalid(field: string): 'true' | undefined {
  return fieldError(field) ? 'true' : undefined;
}
</script>

<template>
  <Panel class="integration-settings" :pt="{ content: { class: 'form-grid' } }">
    <template #header><h2>Integrations</h2></template>
    <p class="section-intro">
      Changes are checked against the real services before they are saved. Nothing is stored
      unless it works.
    </p>
    <Message v-if="message" role="status" severity="success" :closable="false">{{ message }}</Message>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <form class="form-grid" novalidate @submit.prevent="emit('save', build())">
      <Fieldset legend="Market data" :pt="{ content: { class: 'form-grid' } }">
        <div class="form-field">
          <p class="form-field__hint">{{ validatedHint }}</p>
          <label for="integration-eodhd-api-key">EODHD API key</label>
          <input
            id="integration-eodhd-api-key" v-model="eodhdKey" type="password" autocomplete="off"
            maxlength="1024" class="p-inputtext p-component p-inputtext-fluid"
            placeholder="Leave blank to keep the saved key"
            :aria-invalid="invalid('eodhd_api_key')" :aria-describedby="describedBy('eodhd_api_key')">
          <p v-if="fieldError('eodhd_api_key')" id="integration-eodhd_api_key-error" class="integration-settings__field-error">
            {{ fieldError('eodhd_api_key') }}
          </p>
        </div>
        <Message
          v-if="sectionStatus('eodhd')" data-status="eodhd"
          :data-severity="sectionStatus('eodhd')!.severity"
          :severity="sectionStatus('eodhd')!.severity" :closable="false">
          {{ sectionStatus('eodhd')!.text }}
        </Message>
      </Fieldset>

      <Fieldset legend="Email delivery" :pt="{ content: { class: 'form-grid' } }">
        <div class="form-field">
          <label for="integration-smtp-host">SMTP host</label>
          <InputText
            id="integration-smtp-host" v-model="host" autocomplete="off" maxlength="253" required fluid
            :invalid="!!fieldError('smtp_host')" :aria-describedby="describedBy('smtp_host')" />
          <p v-if="fieldError('smtp_host')" id="integration-smtp_host-error" class="integration-settings__field-error">
            {{ fieldError('smtp_host') }}
          </p>
        </div>

        <div class="form-field">
          <label for="integration-smtp-port">SMTP port</label>
          <InputText
            id="integration-smtp-port" v-model="port" inputmode="numeric" pattern="[0-9]{1,5}" maxlength="5" required fluid
            :invalid="!!fieldError('smtp_port')" :aria-describedby="describedBy('smtp_port')" />
          <p v-if="fieldError('smtp_port')" id="integration-smtp_port-error" class="integration-settings__field-error">
            {{ fieldError('smtp_port') }}
          </p>
        </div>

        <div class="form-field">
          <label for="integration-smtp-from">From email</label>
          <InputText
            id="integration-smtp-from" v-model="from" type="email" autocomplete="email" maxlength="320" required fluid
            :invalid="!!fieldError('smtp_from')" :aria-describedby="describedBy('smtp_from')" />
          <p v-if="fieldError('smtp_from')" id="integration-smtp_from-error" class="integration-settings__field-error">
            {{ fieldError('smtp_from') }}
          </p>
        </div>

        <div class="form-field">
          <label for="integration-smtp-username">SMTP username</label>
          <InputText
            id="integration-smtp-username" v-model="username" autocomplete="username" maxlength="320" fluid
            :invalid="!!fieldError('smtp_username')" :aria-describedby="describedBy('smtp_username')" />
          <p v-if="fieldError('smtp_username')" id="integration-smtp_username-error" class="integration-settings__field-error">
            {{ fieldError('smtp_username') }}
          </p>
        </div>

        <div class="form-field">
          <label for="integration-smtp-password">SMTP password</label>
          <p class="form-field__hint">{{ passwordHint }}</p>
          <input
            id="integration-smtp-password" v-model="password" type="password" autocomplete="new-password"
            maxlength="1024" class="p-inputtext p-component p-inputtext-fluid"
            :aria-invalid="invalid('smtp_password')" :aria-describedby="describedBy('smtp_password')">
          <p v-if="fieldError('smtp_password')" id="integration-smtp_password-error" class="integration-settings__field-error">
            {{ fieldError('smtp_password') }}
          </p>
        </div>
        <Message
          v-if="sectionStatus('smtp')" data-status="smtp"
          :data-severity="sectionStatus('smtp')!.severity"
          :severity="sectionStatus('smtp')!.severity" :closable="false">
          {{ sectionStatus('smtp')!.text }}
        </Message>
      </Fieldset>

      <div class="form-actions">
        <Button
          type="button" data-verify severity="secondary" :disabled="busy"
          :label="busy ? 'Please wait…' : 'Check without saving'" @click="emit('verify', build())" />
        <Button type="submit" :disabled="busy" :label="busy ? 'Please wait…' : 'Save changes'" />
      </div>
    </form>
  </Panel>
</template>

<style scoped>
/* Chrome comes from Fieldset and Panel, and spacing from the shared .form-grid/.form-field
   convention in main.css. Only the error text keeps a size of its own here. */
.integration-settings__field-error { margin: 0; font-size: 0.85rem; }
</style>
