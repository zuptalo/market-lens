<script setup lang="ts">
import { computed, ref } from 'vue';
import Button from 'primevue/button';
import Select from 'primevue/select';
import Message from 'primevue/message';
import type { OrderIntent } from '@/types/marketData';

/**
 * Moving something you were considering into the paper account.
 *
 * This is the only way an order comes into existence. There is no control here that asks a strategy
 * what to promote, and no default selection: the product proposes nothing, and a pre-selected
 * intent would be a proposal wearing a different hat.
 */

const props = withDefaults(defineProps<{
  intents: OrderIntent[];
  busy?: boolean;
  refusal?: string;
}>(), { busy: false, refusal: '' });

const emit = defineEmits<{ promote: [intentId: string] }>();

const chosen = ref<string | null>(null);

const choices = computed(() => props.intents.map((intent) => ({
  label: `${intent.direction === 'buy' ? 'Buy' : 'Sell'} ${Number(intent.quantity)
    .toLocaleString(undefined, { maximumFractionDigits: 4 })} ${intent.ticker}`,
  value: intent.id,
})));

function submit(): void {
  if (chosen.value === null) return;
  emit('promote', chosen.value);
  chosen.value = null;
}
</script>

<template>
  <form class="promote-form" aria-labelledby="promote-heading" @submit.prevent="submit">
    <h2 id="promote-heading">Promote something you are considering</h2>
    <p class="promote-form__lead">
      An order here comes from an intent you wrote down, and from nothing else. It will fill at the
      open of the next session that instrument trades — a price nobody has seen yet — and the
      account will charge the trading costs it was opened with.
    </p>

    <Message v-if="props.refusal" severity="warn" :closable="false" data-testid="promote-refusal">
      {{ props.refusal }}
    </Message>

    <Message v-if="props.intents.length === 0" severity="info" :closable="false">
      You are not considering anything at the moment. Write one down on the Intents screen and it
      will be offered here.
    </Message>

    <template v-else>
      <div class="promote-form__field">
        <label for="promote-intent">Which one</label>
        <Select
          input-id="promote-intent"
          v-model="chosen"
          :options="choices"
          option-label="label"
          option-value="value"
          aria-label="Which intent to promote into the paper account"
        />
      </div>
      <Button
        type="submit"
        label="Promote it"
        :disabled="chosen === null"
        :loading="props.busy"
      />
    </template>
  </form>
</template>

<style scoped>
.promote-form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  max-width: 34rem;
}

.promote-form__lead {
  color: var(--p-text-muted-color);
  margin: 0;
  max-width: 60ch;
}

.promote-form__field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.promote-form__field label {
  font-size: 0.875rem;
  font-weight: 600;
}
</style>
