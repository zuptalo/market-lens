<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import Message from 'primevue/message';
import LoadingBlock from '@/components/finance/LoadingBlock.vue';
import { unsubscribeWithToken } from '@/services/notifications';
import type { NotificationChannel, NotificationKind } from '@/types/notifications';

/**
 * Stopping one kind of message, from a link in that message.
 *
 * No sign-in, deliberately. An unsubscribe that requires one is an unsubscribe people do not use:
 * they mark the mail as spam instead, and the sending domain pays for it. The link can do exactly
 * one thing — turn off the kind that sent the message, on the channel it arrived by — and it works
 * the moment it is opened rather than after a confirmation nobody reads.
 */

const route = useRoute();
const working = ref(true);
const stopped = ref<{ kind: NotificationKind; channel: NotificationChannel } | null>(null);
const error = ref('');

const kindWording: Record<NotificationKind, string> = {
  decision_waiting: 'when a decision is waiting for you',
  paper_fill: 'when a paper order settles',
  pipeline_failure: 'when market data does not arrive',
  signal_change: 'when a strategy changes its view',
};

const channelWording: Record<NotificationChannel, string> = {
  email: 'by email',
  web_push: 'by push',
};

onMounted(async () => {
  const token = String(route.query.token ?? '');
  if (!token) {
    working.value = false;
    error.value = 'This link is missing the part that says what to turn off.';
    return;
  }
  try {
    stopped.value = await unsubscribeWithToken(token);
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : 'This link could not be used.';
  } finally {
    working.value = false;
  }
});
</script>

<template>
  <div class="unsubscribe">
    <header class="page-intro">
      <p class="eyebrow">Notifications</p>
      <h1>Turning one thing off</h1>
    </header>

    <LoadingBlock v-if="working" label="Turning it off…" :rows="1" />

    <template v-else-if="stopped">
      <Message severity="success" :closable="false" data-testid="unsubscribed">
        Done. Market Lens will no longer tell you {{ kindWording[stopped.kind] }}
        {{ channelWording[stopped.channel] }}.
      </Message>
      <p class="unsubscribe__detail">
        Nothing else changed. Every other kind of message, and every other channel, is exactly as
        you left it — you can see and change them all under Account settings.
      </p>
    </template>

    <template v-else>
      <Message severity="warn" :closable="false" data-testid="unsubscribe-failed">{{ error }}</Message>
      <p class="unsubscribe__detail">
        You can turn any kind of notification off under Account settings, which needs you to be
        signed in but does not need this link.
      </p>
    </template>
  </div>
</template>

<style scoped>
.unsubscribe__detail {
  color: var(--p-text-muted-color);
  max-width: 60ch;
}
</style>
