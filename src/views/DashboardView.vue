<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import WaitingList, { type WaitingItem } from '@/components/finance/WaitingList.vue';
import ChangeList, { type ChangeItem } from '@/components/finance/ChangeList.vue';
import UnknownList, { type UnknownItem } from '@/components/finance/UnknownList.vue';
import LoadingBlock from '@/components/finance/LoadingBlock.vue';
import {
  MarketDataLive,
  fetchFeatureRuns,
  fetchFindingsAwaitingDecision,
  fetchPortfolio,
  fetchRecentImports,
  fetchRiskLimits,
  fetchStrategyRuns,
  type LiveEventSource,
} from '@/services/marketData';
import { createCoalescer } from '@/services/coalesce';
import type { ConnectionState } from '@/types/marketData';

/**
 * The two questions somebody actually has when they open this: has anything changed, and does
 * anything need me.
 *
 * It composes six reads that already exist and computes nothing of its own. Each of those reads
 * enforces its own ownership boundary — two private, four shared — which is the argument against an
 * aggregating endpoint that would have to re-derive the same distinctions somewhere else.
 *
 * Nothing here is a value or a percentage. Every figure on a dashboard is a second copy of
 * something another screen owns, and this is the screen where that discipline is most likely to be
 * quietly abandoned.
 */

const waiting = ref<WaitingItem[]>([]);
const changed = ref<ChangeItem[]>([]);
const unknown = ref<UnknownItem[]>([]);
/** Sources that failed. Named, because a failed read is not an all-clear. */
const unreadable = ref<string[]>([]);
const changeUnreadable = ref<string[]>([]);
const loading = ref(true);
const connectionState = ref<ConnectionState>(navigator.onLine ? 'reconnecting' : 'offline');

let controller: AbortController | undefined;

/** A date a person can place, without a time nobody needs. */
function on(value: string | null): string {
  if (!value) return 'at an unrecorded time';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? 'at an unrecorded time' : `on ${parsed.toISOString().slice(0, 10)}`;
}

function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

async function load(): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;

  const nextWaiting: WaitingItem[] = [];
  const nextChanged: ChangeItem[] = [];
  const nextUnknown: UnknownItem[] = [];
  const failed: string[] = [];
  const changeFailed: string[] = [];

  // Settled rather than awaited together: one source failing must not blank the screen, and which
  // one failed is itself something the reader needs to know.
  const [findings, limits, holdings, imports, features, strategies] = await Promise.allSettled([
    fetchFindingsAwaitingDecision(fetch, signal),
    fetchRiskLimits(fetch, signal),
    fetchPortfolio(fetch, signal),
    fetchRecentImports(fetch, signal),
    fetchFeatureRuns(fetch, signal),
    fetchStrategyRuns(fetch, signal),
  ]);
  if (signal.aborted) return;

  if (findings.status === 'fulfilled') {
    if (findings.value.length > 0) {
      nextWaiting.push({
        label: plural(findings.value.length, 'finding awaits your decision', 'findings await your decision'),
        detail: 'Asking the source again did not change these answers, so no further run can settle '
          + 'them. The product does not decide whether a limitation of the data is acceptable.',
        to: '/operations',
        linkLabel: 'Settle them on Operations',
      });
    }
  } else {
    failed.push('The quality findings');
  }

  if (limits.status === 'fulfilled') {
    const exceeded = limits.value.limits.filter((limit) => limit.state === 'exceeded');
    const unevaluable = limits.value.limits.filter((limit) => limit.state === 'unevaluable');
    if (exceeded.length > 0) {
      nextWaiting.push({
        label: plural(exceeded.length, 'limit you are outside', 'limits you are outside'),
        detail: 'These are your own rules. The product reports where you stand and says nothing '
          + 'about what to do next.',
        to: '/risk',
        linkLabel: 'See where you stand',
      });
    }
    if (unevaluable.length > 0) {
      nextUnknown.push({
        label: plural(unevaluable.length, 'limit could not be evaluated', 'limits could not be evaluated'),
        detail: 'A limit measured against a portfolio total cannot be evaluated while a holding '
          + 'has no price. It is not reported as satisfied.',
        to: '/risk',
        linkLabel: 'See which',
      });
    }
  } else {
    failed.push('Your limits');
  }

  if (holdings.status === 'fulfilled') {
    const unvalued = holdings.value.holdings.filter((holding) => holding.valuation.value === null);
    if (unvalued.length > 0) {
      nextUnknown.push({
        label: plural(unvalued.length, 'holding could not be valued', 'holdings could not be valued'),
        detail: 'Your total is reported as incomplete rather than leaving them out, because a total '
          + 'that quietly omitted a holding would look whole.',
        to: '/portfolio',
        linkLabel: 'See which',
      });
    }
  } else {
    failed.push('Your portfolio');
  }

  if (imports.status === 'fulfilled') {
    const [latest] = imports.value;
    if (latest) {
      if (latest.status === 'failed' || latest.status === 'partial') {
        // A run that did not finish cleanly belongs among the things waiting, not among the things
        // that merely happened.
        nextWaiting.push({
          label: `The last import ended ${latest.status}`,
          detail: 'Some instruments were left with the data they had before it ran.',
          to: '/operations',
          linkLabel: 'See what happened',
        });
      } else {
        const corrected = latest.counts.revised ?? 0;
        nextChanged.push({
          label: `Market data last arrived ${on(latest.finishedAt ?? latest.startedAt)}`,
          // Corrections are the statistic worth surfacing: a restated session moves every value
          // derived from it. Sessions stored is almost always the same number.
          detail: corrected > 0
            ? `${plural(corrected, 'stored session was corrected', 'stored sessions were corrected')}, `
              + 'so everything derived from them moved too.'
            : 'No stored session was corrected.',
          to: '/operations',
          linkLabel: 'See the run',
        });
      }
    }
  } else {
    changeFailed.push('The import history');
  }

  if (features.status === 'fulfilled') {
    const [latest] = features.value;
    if (latest) {
      nextChanged.push({
        label: `Statistics last computed ${on(latest.finishedAt ?? latest.startedAt)}`,
        detail: 'The returns, trend and volatility every other screen reads.',
        to: '/operations',
        linkLabel: 'See the runs',
      });
    }
  } else {
    changeFailed.push('The feature runs');
  }

  if (strategies.status === 'fulfilled') {
    const [latest] = strategies.value;
    if (latest) {
      nextChanged.push({
        label: `Signals last computed ${on(latest.finishedAt ?? latest.startedAt)}`,
        detail: 'What the published strategy made of the universe, as of then.',
        to: '/signals',
        linkLabel: 'Read the ranking',
      });
    }
  } else {
    changeFailed.push('The strategy runs');
  }

  waiting.value = nextWaiting;
  changed.value = nextChanged;
  unknown.value = nextUnknown;
  unreadable.value = failed;
  changeUnreadable.value = changeFailed;
  loading.value = false;
}

const anythingLoaded = computed(() => !loading.value);

/**
 * Every item here derives from something that already publishes a change — an import, a
 * recomputation, a finding, a portfolio or a limit. A new event type would fire in exactly the same
 * circumstances and carry nothing extra.
 */
const refresh = createCoalescer(async () => { await load(); });

function browserEventSource(url: string, lastEventId: string): LiveEventSource {
  const endpoint = lastEventId ? `${url}?last_event_id=${encodeURIComponent(lastEventId)}` : url;
  return new EventSource(endpoint, { withCredentials: true }) as unknown as LiveEventSource;
}

const live = new MarketDataLive({
  sourceFactory: browserEventSource,
  onRefresh: (entityType) => { refresh.add(entityType); },
  onState: (state) => { connectionState.value = state; },
  reconnectDelayMs: 1_000,
  staleAfterMs: 10_000,
});
const online = () => live.setOnline(true);
const offline = () => live.setOnline(false);

onMounted(async () => {
  await load();
  live.setOnline(navigator.onLine);
  live.start();
  window.addEventListener('online', online);
  window.addEventListener('offline', offline);
});

onBeforeUnmount(() => {
  controller?.abort();
  refresh.cancel();
  live.stop();
  window.removeEventListener('online', online);
  window.removeEventListener('offline', offline);
});
</script>

<template>
  <div class="overview">
    <header class="page-intro">
      <p class="eyebrow">Overview</p>
      <h1 id="page-title">What needs you</h1>
      <p class="overview__lead">
        Everything here links to the screen that owns it, which is where the figures are. This page
        counts and dates; it does not restate.
      </p>
    </header>

    <LoadingBlock v-if="loading" label="Checking what needs you…" :rows="4" />

    <template v-else-if="anythingLoaded">
      <WaitingList :items="waiting" :unreadable="unreadable" />
      <ChangeList :items="changed" :unreadable="changeUnreadable" />
      <UnknownList :items="unknown" />
    </template>
  </div>
</template>

<style scoped>
.overview {
  display: grid;
  gap: 2.5rem;
}

.overview__lead {
  color: var(--p-text-muted-color);
  max-width: 70ch;
}
</style>
