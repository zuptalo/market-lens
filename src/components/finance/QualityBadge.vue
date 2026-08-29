<script setup lang="ts">
import { computed } from 'vue';
import type { ImportStatus } from '@/types/marketData';

const props = defineProps<{ status: ImportStatus }>();
const severityClass = computed(() => ({
  succeeded: 'severity-success',
  partial: 'severity-warning',
  failed: 'severity-error',
  cancelled: 'severity-error',
  queued: 'severity-secondary',
  running: 'severity-info',
}[props.status]));
</script>

<template>
  <span class="quality-badge" :class="severityClass">{{ status }}</span>
</template>
