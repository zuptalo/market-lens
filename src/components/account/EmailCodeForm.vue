<script setup lang="ts">
import { computed, nextTick, ref } from 'vue';
import InputText from 'primevue/inputtext';
import Button from 'primevue/button';
import Message from 'primevue/message';

const props = defineProps<{
  email: string;
  busy?: boolean;
  error?: string | null;
  message?: string | null;
  resendIn?: number;
}>();
const emit = defineEmits<{
  submit: [value: { email: string; code: string }];
  resend: [];
}>();

const code = ref('');
const localError = ref<string | null>(null);
const canResend = computed(() => (props.resendIn ?? 0) <= 0);

// People paste codes straight out of an email, which commonly carries spaces, hyphens, or a
// trailing newline. The field therefore accepts more raw characters than a code has digits -
// a maxlength of six would truncate "01 23-45" to "01 23-" and silently drop real digits -
// and normalisation on input reduces whatever arrives to at most six digits.
function normalise(value: string): string {
  return value.replace(/\D/g, '').slice(0, 6);
}

// Normalising has to survive the component's own render. PrimeVue writes the raw typed value
// back to the element as it re-renders, and when the cleaned value equals the one already held
// there is no further render to correct it - so the element is fixed up after that render
// rather than during the event, which is what keeps a pasted "01 23-45" showing as "012345".
async function onInput(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const cleaned = normalise(input.value);
  code.value = cleaned;
  if (cleaned.length === 6) localError.value = null;
  await nextTick();
  if (input.value !== cleaned) input.value = cleaned;
}

function submit(): void {
  if (code.value.length !== 6) {
    localError.value = 'Enter the six digits from your email.';
    return;
  }
  localError.value = null;
  emit('submit', { email: props.email, code: code.value });
}
</script>

<template>
  <form class="email-code-form" novalidate @submit.prevent="submit">
    <h1>Enter your passcode</h1>
    <p class="email-code-form__recipient">
      We sent a six-digit code to <strong>{{ email }}</strong>.
    </p>
    <Message v-if="message" role="status" severity="info" :closable="false">{{ message }}</Message>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <Message v-else-if="localError" severity="error" :closable="false">{{ localError }}</Message>

    <label for="member-code">Six-digit passcode</label>
    <InputText
      id="member-code"
      name="code"
      v-model="code"
      inputmode="numeric"
      autocomplete="one-time-code"
      pattern="[0-9]{6}"
      aria-label="Six-digit passcode"
      maxlength="20"
      required
      aria-describedby="member-code-hint"
      fluid
      @input="onInput"
    />
    <p id="member-code-hint" class="email-code-form__hint">The code expires in 10 minutes and can be used once.</p>

    <Button type="submit" :disabled="busy" :label="busy ? 'Verifying…' : 'Verify passcode'" />
    <Button type="button" data-resend severity="secondary" :disabled="busy || !canResend" @click="emit('resend')">
      <template v-if="canResend">Send a new code</template>
      <template v-else>Send a new code in {{ resendIn }}s</template>
    </Button>
  </form>
</template>
