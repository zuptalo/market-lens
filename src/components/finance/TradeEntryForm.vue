<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import Button from 'primevue/button';
import InputNumber from 'primevue/inputnumber';
import DatePicker from 'primevue/datepicker';
import Select from 'primevue/select';
import Message from 'primevue/message';
import type { PortfolioTrade, TradeInput } from '@/types/marketData';

/** Only what the picker needs. Taking a full instrument type would couple this form to whichever
 *  listing shape happened to be passed in, and it reads none of the rest. */
interface Choosable {
  id: string;
  ticker: string;
  name: string;
}

/**
 * Recording a purchase or a sale, and correcting one.
 *
 * The same form does both, because a correction is not a different kind of fact: it is the same
 * trade said again, more accurately. What changes is the heading and what the product does with the
 * result — the earlier version is superseded rather than overwritten.
 */

const props = withDefaults(defineProps<{
  instruments: Choosable[];
  editing?: PortfolioTrade | null;
  busy?: boolean;
  refusal?: string;
}>(), { editing: null, busy: false, refusal: '' });

const emit = defineEmits<{
  submit: [input: TradeInput];
  cancel: [];
}>();

const instrumentId = ref('');
const direction = ref<'buy' | 'sell'>('buy');
const quantity = ref<number | null>(null);
const price = ref<number | null>(null);
const costs = ref<number | null>(null);
const tradeDate = ref<Date | null>(new Date());

const directions = [
  { label: 'Bought', value: 'buy' },
  { label: 'Sold', value: 'sell' },
];

const instrumentOptions = computed(() => props.instruments.map((instrument) => ({
  label: `${instrument.ticker} — ${instrument.name}`,
  value: instrument.id,
})));

const heading = computed(() => (props.editing ? 'Correct this trade' : 'Record a trade'));

/** A trade cannot be dated in the future: a holding you do not have yet is not a holding. */
const today = new Date();

watch(() => props.editing, (trade) => {
  if (!trade) {
    instrumentId.value = '';
    direction.value = 'buy';
    quantity.value = null;
    price.value = null;
    costs.value = null;
    tradeDate.value = new Date();
    return;
  }
  instrumentId.value = trade.instrumentId;
  direction.value = trade.direction;
  quantity.value = Number(trade.quantity);
  price.value = Number(trade.price);
  costs.value = Number(trade.costs);
  tradeDate.value = new Date(trade.tradeDate);
}, { immediate: true });

const complete = computed(() => instrumentId.value !== ''
  && quantity.value !== null && quantity.value > 0
  && price.value !== null && price.value > 0
  && tradeDate.value !== null);

function submit(): void {
  if (!complete.value || tradeDate.value === null) return;
  emit('submit', {
    instrumentId: instrumentId.value,
    direction: direction.value,
    quantity: String(quantity.value),
    price: String(price.value),
    costs: String(costs.value ?? 0),
    tradeDate: tradeDate.value.toISOString().slice(0, 10),
  });
}
</script>

<template>
  <form class="entry" aria-labelledby="entry-heading" @submit.prevent="submit">
    <h2 id="entry-heading">{{ heading }}</h2>

    <!-- A refusal is shown where the person is working, with what to do about it. "Something went
         wrong" is not an answer to "why can I not record this". -->
    <Message v-if="props.refusal" severity="warn" :closable="false" data-testid="trade-refusal">
      {{ props.refusal }}
    </Message>

    <div class="entry__field">
      <label for="entry-instrument">Instrument</label>
      <!-- Named explicitly: the rendered combobox is announced by its current value, so without
           this a screen reader hears "Choose from the tracked universe" and never "Instrument". -->
      <Select
        input-id="entry-instrument"
        v-model="instrumentId"
        :options="instrumentOptions"
        option-label="label"
        option-value="value"
        filter
        aria-label="Instrument"
        placeholder="Choose from the tracked universe"
      />
      <small>This product tracks a curated universe of Nordic listings.</small>
    </div>

    <div class="entry__field">
      <label for="entry-direction">Bought or sold</label>
      <Select
        input-id="entry-direction"
        v-model="direction"
        :options="directions"
        option-label="label"
        option-value="value"
        aria-label="Bought or sold"
      />
    </div>

    <div class="entry__row">
      <div class="entry__field">
        <label for="entry-quantity">Shares</label>
        <InputNumber input-id="entry-quantity" v-model="quantity" :min-fraction-digits="0" :max-fraction-digits="6" />
      </div>
      <div class="entry__field">
        <label for="entry-price">Price per share</label>
        <InputNumber input-id="entry-price" v-model="price" :min-fraction-digits="2" :max-fraction-digits="6" />
        <small>In the instrument's own currency, as your broker shows it.</small>
      </div>
    </div>

    <div class="entry__row">
      <div class="entry__field">
        <label for="entry-costs">What the trade cost</label>
        <InputNumber input-id="entry-costs" v-model="costs" :min-fraction-digits="2" :max-fraction-digits="6" />
        <small>Brokerage and fees. Leave at zero if there were none.</small>
      </div>
      <div class="entry__field">
        <label for="entry-date">Date</label>
        <DatePicker input-id="entry-date" v-model="tradeDate" date-format="yy-mm-dd" :max-date="today" />
      </div>
    </div>

    <div class="entry__actions">
      <Button type="submit" label="Save" :disabled="!complete" :loading="props.busy" />
      <Button
        v-if="props.editing"
        type="button"
        label="Cancel"
        severity="secondary"
        text
        @click="$emit('cancel')"
      />
    </div>
  </form>
</template>

<style scoped>
.entry {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  max-width: 44rem;
}

.entry__row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 12rem), 1fr));
  gap: 1rem;
}

.entry__field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.entry__field label {
  font-size: 0.875rem;
  font-weight: 600;
}

.entry__field small {
  color: var(--p-text-muted-color);
}

.entry__actions {
  display: flex;
  gap: 0.75rem;
}
</style>
