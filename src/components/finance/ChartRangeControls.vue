<script setup lang="ts">
import { computed } from 'vue';
import Button from 'primevue/button';

/**
 * Range, zoom, pan and overlay controls for the price chart.
 *
 * Everything here is a real button. Pinch and drag work on the chart itself, but they are an
 * addition rather than the mechanism: a control that exists only as a gesture is unreachable
 * by keyboard and invisible to anyone who does not already know it is there (SC-008).
 *
 * Ranges are counted in stored exchange sessions and labelled as such. "30 days" would mean
 * a different number of observations on Stockholm and Oslo in a week containing a Norwegian
 * holiday, so the count a reader sees is the count they get (research R7).
 */

const props = defineProps<{
  sessions: number;
  overlays: number[];
  coverageSessions: number;
}>();

defineEmits<{
  (event: 'range', sessions: number): void;
  (event: 'zoom', direction: 'in' | 'out'): void;
  (event: 'pan', direction: 'back' | 'forward'): void;
  (event: 'toggle-overlay', windowLength: number): void;
}>();

const RANGES = [
  { label: '20 sessions', sessions: 20 },
  { label: '60 sessions', sessions: 60 },
  { label: '120 sessions', sessions: 120 },
  { label: '250 sessions', sessions: 250 },
  { label: '1250 sessions', sessions: 1250 },
];

const OVERLAY_WINDOWS = [20, 50, 200];

/**
 * Only ranges the instrument can actually fill are offered. Showing a year for an instrument
 * with forty stored sessions makes the chart look empty and invites the reader to conclude
 * data is missing when it simply never existed.
 */
const availableRanges = computed(() =>
  RANGES.filter((range, index) =>
    range.sessions <= props.coverageSessions || RANGES[index - 1]?.sessions < props.coverageSessions),
);

const availableOverlays = computed(() =>
  OVERLAY_WINDOWS.filter((windowLength) => windowLength < props.coverageSessions));
</script>

<template>
  <div class="chart-controls">
    <div class="control-group" role="group" aria-label="Chart range">
      <Button
        v-for="range in availableRanges"
        :key="range.sessions"
        type="button"
        size="small"
        :label="range.label"
        :outlined="props.sessions !== range.sessions"
        :aria-pressed="props.sessions === range.sessions"
        @click="$emit('range', range.sessions)"
      />
    </div>

    <div class="control-group" role="group" aria-label="Zoom and pan">
      <Button type="button" size="small" outlined label="←" aria-label="Pan back" @click="$emit('pan', 'back')" />
      <Button type="button" size="small" outlined label="−" aria-label="Zoom out" @click="$emit('zoom', 'out')" />
      <Button type="button" size="small" outlined label="+" aria-label="Zoom in" @click="$emit('zoom', 'in')" />
      <Button type="button" size="small" outlined label="→" aria-label="Pan forward" @click="$emit('pan', 'forward')" />
    </div>

    <div class="control-group" role="group" aria-label="Moving averages">
      <Button
        v-for="windowLength in availableOverlays"
        :key="windowLength"
        type="button"
        size="small"
        :label="`MA${windowLength}`"
        :outlined="!props.overlays.includes(windowLength)"
        :aria-label="`${windowLength}-session moving average`"
        :aria-pressed="props.overlays.includes(windowLength)"
        @click="$emit('toggle-overlay', windowLength)"
      />
    </div>
  </div>
</template>

<style scoped>
/*
 * Layout only. The selected state is carried by aria-pressed and shown through the library's
 * own outlined/solid variants, so nothing here restyles chrome the theme owns.
 */
.chart-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
  margin-block: 0.75rem;
}

.control-group {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

@media (max-width: 767px) {
  .chart-controls {
    gap: 0.75rem;
  }

  /* A row of touch targets that scrolls itself rather than wrapping into a tall block. */
  .control-group {
    overflow-x: auto;
    flex-wrap: nowrap;
    padding-block-end: 0.25rem;
  }
}
</style>
