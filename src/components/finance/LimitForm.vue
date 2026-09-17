<script setup lang="ts">
import { computed, ref } from 'vue';
import Button from 'primevue/button';
import InputNumber from 'primevue/inputnumber';
import Select from 'primevue/select';
import Message from 'primevue/message';
import type { LimitKind } from '@/types/marketData';

/**
 * Writing down a rule.
 *
 * The form offers no default threshold and no placeholder value that could be mistaken for one. It
 * asks what the person's rule is; it has no view on what it should be.
 */

const props = withDefaults(defineProps<{
  busy?: boolean;
  refusal?: string;
}>(), { busy: false, refusal: '' });

const emit = defineEmits<{ submit: [kind: LimitKind, threshold: string] }>();

const kinds = [
  { label: 'Most in any one company', value: 'instrument_share' as LimitKind },
  { label: 'Most in any one sector', value: 'sector_share' as LimitKind },
  { label: 'Most in any one market', value: 'market_share' as LimitKind },
  { label: 'Most holdings at once', value: 'holding_count' as LimitKind },
];

const kind = ref<LimitKind>('instrument_share');
const percent = ref<number | null>(null);
const count = ref<number | null>(null);

const isCount = computed(() => kind.value === 'holding_count');
const complete = computed(() => (isCount.value
  ? count.value !== null && count.value >= 1
  : percent.value !== null && percent.value > 0 && percent.value <= 100));

function submit(): void {
  if (!complete.value) return;
  // A share is entered as a percentage because that is how people say it, and stored as a
  // proportion because that is what it is.
  const threshold = isCount.value
    ? String(Math.round(count.value ?? 0))
    : String((percent.value ?? 0) / 100);
  emit('submit', kind.value, threshold);
}
</script>

<template>
  <form class="limit-form" aria-labelledby="limit-form-heading" @submit.prevent="submit">
    <h2 id="limit-form-heading">Set a limit</h2>
    <p class="limit-form__lead">
      A limit is your own rule. Setting one here does not stop you doing anything — the product will
      simply tell you when you are outside it.
    </p>

    <Message v-if="props.refusal" severity="warn" :closable="false" data-testid="limit-refusal">
      {{ props.refusal }}
    </Message>

    <div class="limit-form__field">
      <label for="limit-kind">What the rule is about</label>
      <Select
        input-id="limit-kind"
        v-model="kind"
        :options="kinds"
        option-label="label"
        option-value="value"
        aria-label="What the rule is about"
      />
    </div>

    <div v-if="!isCount" class="limit-form__field">
      <label for="limit-percent">No more than</label>
      <InputNumber
        input-id="limit-percent"
        v-model="percent"
        suffix=" %"
        :min="0"
        :max="100"
        :max-fraction-digits="2"
      />
    </div>
    <div v-else class="limit-form__field">
      <label for="limit-count">No more than</label>
      <InputNumber input-id="limit-count" v-model="count" :min="1" :max-fraction-digits="0" />
      <small>holdings at once</small>
    </div>

    <Button type="submit" label="Set this limit" :disabled="!complete" :loading="props.busy" />
  </form>
</template>

<style scoped>
.limit-form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  max-width: 34rem;
}

.limit-form__lead {
  color: var(--p-text-muted-color);
  margin: 0;
  max-width: 60ch;
}

.limit-form__field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.limit-form__field label {
  font-size: 0.875rem;
  font-weight: 600;
}

.limit-form__field small {
  color: var(--p-text-muted-color);
}
</style>
