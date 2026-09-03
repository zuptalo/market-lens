<script setup lang="ts">
import Card from 'primevue/card';
</script>

<template>
  <div class="overview" aria-labelledby="page-title">
    <!-- The header keeps a narrow measure like every other tab; what follows spans the page. -->
    <header class="hero page-intro">
      <p class="eyebrow">Foundation stage</p>
      <h1 id="page-title">Market Lens</h1>
      <p>Stock research and strategy experimentation platform</p>
    </header>
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
  </div>
</template>

<style scoped>
.overview {
  display: grid;
  gap: 2rem;
}
</style>
