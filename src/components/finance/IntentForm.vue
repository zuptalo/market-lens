<script setup lang="ts">
import { computed, ref } from 'vue';
import Button from 'primevue/button';
import InputNumber from 'primevue/inputnumber';
import Select from 'primevue/select';
import Message from 'primevue/message';
import type { IntentDirection, IntentInput } from '@/types/marketData';

/**
 * Writing down something you are considering.
 *
 * The form offers no default quantity, no default price and no suggested instrument. It asks what
 * the person is considering; it has no view on what they should be. Nothing here is an order: there
 * is no venue, no order type, no time in force and no destination, and the absence is the
 * safeguard — a form carrying an order type would be one field away from being transmissible.
 */

const props = withDefaults(defineProps<{
  instruments: { id: string; ticker: string; name: string }[];
  busy?: boolean;
  refusal?: string;
  /** Set when the list could not be loaded, so the field says so rather than waiting forever. */
  instrumentsError?: string;
}>(), { busy: false, refusal: '', instrumentsError: '' });

const emit = defineEmits<{ submit: [input: IntentInput] }>();

const directions = [
  { label: 'Buy', value: 'buy' as IntentDirection },
  { label: 'Sell', value: 'sell' as IntentDirection },
];

const instrumentId = ref<string | null>(null);
const direction = ref<IntentDirection>('buy');
const quantity = ref<number | null>(null);
const price = ref<number | null>(null);
const costs = ref<number | null>(null);

const choices = computed(() => props.instruments.map((instrument) => ({
  label: `${instrument.ticker} — ${instrument.name}`,
  value: instrument.id,
})));

/**
 * Opening the list before it has arrived shows an empty dropdown that does not repopulate when the
 * data lands, so the field waits instead of claiming there is nothing to choose.
 */
const instrumentsReady = computed(() => props.instruments.length > 0);

const complete = computed(() => instrumentId.value !== null
  && quantity.value !== null && quantity.value > 0
  && price.value !== null && price.value > 0);

function submit(): void {
  if (!complete.value) return;
  emit('submit', {
    instrumentId: instrumentId.value ?? '',
    direction: direction.value,
    quantity: String(quantity.value ?? 0),
    price: String(price.value ?? 0),
    costs: String(costs.value ?? 0),
  });
}
</script>

<template>
  <form class="intent-form" aria-labelledby="intent-form-heading" @submit.prevent="submit">
    <h2 id="intent-form-heading">Write down something you are considering</h2>
    <p class="intent-form__lead">
      Nothing is sent anywhere. This product has no broker connection and places no orders — writing
      this down tells you what it would do to your holdings and to the limits you set yourself, and
      nothing else happens until you decide.
    </p>

    <Message v-if="props.refusal" severity="warn" :closable="false" data-testid="intent-refusal">
      {{ props.refusal }}
    </Message>

    <div class="intent-form__field">
      <label for="intent-instrument">Which one</label>
      <Select
        input-id="intent-instrument"
        v-model="instrumentId"
        :options="choices"
        option-label="label"
        option-value="value"
        filter
        :disabled="!instrumentsReady"
        aria-label="Which instrument you are considering"
      />
      <small v-if="props.instrumentsError">{{ props.instrumentsError }}</small>
      <small v-else-if="!instrumentsReady">The list of instruments is still loading.</small>
    </div>

    <div class="intent-form__field">
      <label for="intent-direction">Buy or sell</label>
      <Select
        input-id="intent-direction"
        v-model="direction"
        :options="directions"
        option-label="label"
        option-value="value"
        aria-label="Whether you are considering buying or selling"
      />
    </div>

    <div class="intent-form__field">
      <label for="intent-quantity">How many</label>
      <InputNumber
        input-id="intent-quantity"
        v-model="quantity"
        :min="0"
        :max-fraction-digits="6"
      />
    </div>

    <div class="intent-form__field">
      <label for="intent-price">At what price, per share</label>
      <InputNumber
        input-id="intent-price"
        v-model="price"
        :min="0"
        :max-fraction-digits="6"
      />
      <small>In the instrument's own currency.</small>
    </div>

    <div class="intent-form__field">
      <label for="intent-costs">What you expect it to cost you</label>
      <InputNumber
        input-id="intent-costs"
        v-model="costs"
        :min="0"
        :max-fraction-digits="6"
      />
      <small>Commission and fees, if you know them. Leave it empty if you do not.</small>
    </div>

    <Button type="submit" label="Write this down" :disabled="!complete" :loading="props.busy" />
  </form>
</template>

<style scoped>
.intent-form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  max-width: 34rem;
}

.intent-form__lead {
  color: var(--p-text-muted-color);
  margin: 0;
  max-width: 60ch;
}

.intent-form__field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.intent-form__field label {
  font-size: 0.875rem;
  font-weight: 600;
}

.intent-form__field small {
  color: var(--p-text-muted-color);
}
</style>
