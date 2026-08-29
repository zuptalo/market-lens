<script setup lang="ts">
import type { DailyBarSummary, HistoryCoverage, InstrumentSummary, QualitySummary } from '@/types/marketData';

withDefaults(defineProps<{
  instrument: InstrumentSummary;
  latestBar?: DailyBarSummary | null;
  history?: HistoryCoverage;
  qualitySummary?: QualitySummary;
}>(), {
  latestBar: null,
  history: () => ({ firstSession: null, lastSession: null, barCount: 0 }),
  qualitySummary: () => ({ openWarnings: 0, openErrors: 0 }),
});
</script>

<template>
  <article class="instrument-identity">
    <header>
      <p class="eyebrow">{{ instrument.ticker }} · {{ instrument.exchange.mic }}</p>
      <h2>{{ instrument.name }}</h2>
      <p class="identity-meta">
        <span>{{ instrument.isin }}</span>
        <span>{{ instrument.exchange.name }}</span>
        <span>{{ instrument.currency }} · {{ instrument.country }}</span>
      </p>
    </header>

    <section v-if="latestBar" class="latest-known" aria-labelledby="latest-known-heading">
      <div>
        <p class="eyebrow">Stored daily history</p>
        <h3 id="latest-known-heading">Latest known daily value</h3>
        <p>Session {{ latestBar.sessionDate }} · observed {{ new Date(latestBar.observedAt).toLocaleString() }}</p>
      </div>
      <p class="latest-close">{{ latestBar.close }} {{ latestBar.currency }}</p>
      <dl class="ohlcv-grid">
        <div><dt>Open</dt><dd>{{ latestBar.open }}</dd></div>
        <div><dt>High</dt><dd>{{ latestBar.high }}</dd></div>
        <div><dt>Low</dt><dd>{{ latestBar.low }}</dd></div>
        <div><dt>Close</dt><dd>{{ latestBar.close }}</dd></div>
        <div><dt>Volume</dt><dd>{{ latestBar.volume.toLocaleString() }}</dd></div>
        <div><dt>Source</dt><dd>{{ latestBar.provider }}</dd></div>
      </dl>
      <p v-if="history.firstSession && history.lastSession">
        Coverage: {{ history.firstSession }} to {{ history.lastSession }} · {{ history.barCount }} daily bars
      </p>
      <p data-testid="quality-summary" :class="{ 'status-error': qualitySummary.openErrors > 0 }">
        {{ qualitySummary.openWarnings }} open warning{{ qualitySummary.openWarnings === 1 ? '' : 's' }} ·
        {{ qualitySummary.openErrors }} open error{{ qualitySummary.openErrors === 1 ? '' : 's' }}
      </p>
    </section>
    <p v-else class="empty-state" role="status">
      No daily history is available for this instrument. No current value has been inferred.
    </p>
  </article>
</template>
