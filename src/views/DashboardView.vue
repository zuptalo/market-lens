<script setup lang="ts">
import Card from 'primevue/card';
import Tag from 'primevue/tag';
</script>

<template>
  <section class="hero" aria-labelledby="page-title">
    <Tag value="Foundation stage" severity="secondary" />
    <h1 id="page-title">Market Lens</h1>
    <p>Stock research and strategy experimentation platform</p>
    <Card class="foundation-card">
      <template #title>Ready for specification-driven development</template>
      <template #content>
        <p>
          The application shell and service infrastructure are in place. Market data,
          signals, backtesting, portfolios, and paper trading will be implemented from
          future specifications.
        </p>
      </template>
    </Card>
  </section>
</template>
