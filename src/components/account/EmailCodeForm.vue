<script setup lang="ts">
import { computed, ref } from 'vue';

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

function onInput(event: Event): void {
  const input = event.target as HTMLInputElement;
  const cleaned = normalise(input.value);
  input.value = cleaned;
  code.value = cleaned;
  if (cleaned.length === 6) localError.value = null;
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
    <p v-if="message" role="status">{{ message }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-else-if="localError" role="alert">{{ localError }}</p>

    <label for="member-code">Six-digit passcode</label>
    <input
      id="member-code"
      name="code"
      :value="code"
      inputmode="numeric"
      autocomplete="one-time-code"
      pattern="[0-9]{6}"
      aria-label="Six-digit passcode"
      maxlength="20"
      required
      aria-describedby="member-code-hint"
      @input="onInput"
    >
    <p id="member-code-hint" class="email-code-form__hint">The code expires in 10 minutes and can be used once.</p>

    <button type="submit" :disabled="busy">{{ busy ? 'Verifying…' : 'Verify passcode' }}</button>
    <button type="button" data-resend :disabled="busy || !canResend" @click="emit('resend')">
      <template v-if="canResend">Send a new code</template>
      <template v-else>Send a new code in {{ resendIn }}s</template>
    </button>
  </form>
</template>
