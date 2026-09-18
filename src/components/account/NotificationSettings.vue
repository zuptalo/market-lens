<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import Button from 'primevue/button';
import DatePicker from 'primevue/datepicker';
import Message from 'primevue/message';
import Panel from 'primevue/panel';
import Select from 'primevue/select';
import ToggleSwitch from 'primevue/toggleswitch';
import { formatDateTime } from '@/utils/datetime';
import { authStore } from '@/stores/auth';
import {
  fetchNotificationSettings,
  fetchSubscriptions,
  pushIsAvailable,
  revokeSubscription,
  setNotificationPreference,
  setQuietHours,
  subscribeThisDevice,
} from '@/services/notifications';
import type {
  NotificationChannel,
  NotificationKind,
  NotificationSettings,
  PushSubscriptionSummary,
} from '@/types/notifications';

/**
 * What this person has asked to be told about.
 *
 * Every switch starts off and stays off until somebody moves it. The section says so at the top,
 * because a row of switches that are all off reads as an oversight unless it is stated as a choice.
 *
 * Nothing here suggests turning anything on. A product that nudges somebody toward more
 * notifications is optimising for its own engagement, which is not a thing this product does.
 */

const settings = ref<NotificationSettings | null>(null);
const devices = ref<PushSubscriptionSummary[]>([]);
const loading = ref(true);
const busy = ref(false);
const error = ref('');
const notice = ref('');

let controller: AbortController | undefined;

// What each kind is, said the way a person would say it rather than the way it is stored.
const kindWording: Record<NotificationKind, { title: string; detail: string }> = {
  decision_waiting: {
    title: 'A decision is waiting',
    detail: 'Something only you can settle — a finding this product will not decide, or a rule you set that is no longer being met.',
  },
  paper_fill: {
    title: 'A paper order settled',
    detail: 'An order you promoted filled, or could not. It happens overnight, with nobody watching.',
  },
  pipeline_failure: {
    title: 'Market data did not arrive',
    detail: 'An import did not complete, so some screens are reading older data than they look like they are.',
  },
  signal_change: {
    title: 'A strategy changed its view',
    detail: 'What a strategy makes of an instrument changed. It is a strategy output, not advice, and this product has no view on what to do about it.',
  },
};

const channelWording: Record<NotificationChannel, string> = {
  email: 'Email',
  web_push: 'Push',
};

const kinds = computed(() => {
  const seen: NotificationKind[] = [];
  for (const preference of settings.value?.preferences ?? []) {
    if (!seen.includes(preference.kind)) seen.push(preference.kind);
  }
  return seen;
});

function enabled(kind: NotificationKind, channel: NotificationChannel): boolean {
  return (settings.value?.preferences ?? [])
    .some((preference) => preference.kind === kind && preference.channel === channel && preference.enabled);
}

// Quiet hours, as a pair of times and a zone.
const quietFrom = ref<Date | null>(null);
const quietTo = ref<Date | null>(null);
const quietZone = ref<string>(Intl.DateTimeFormat().resolvedOptions().timeZone);
const zones = computed(() => {
  const candidates = new Set<string>([quietZone.value, 'UTC', 'Europe/Stockholm', 'Europe/London']);
  if (settings.value?.quietHours) candidates.add(settings.value.quietHours.timezone);
  return [...candidates].map((zone) => ({ label: zone, value: zone }));
});

function clockToDate(value: string): Date | null {
  const [hours, minutes] = value.split(':').map(Number);
  if (Number.isNaN(hours) || Number.isNaN(minutes)) return null;
  const when = new Date();
  when.setHours(hours, minutes, 0, 0);
  return when;
}

function dateToClock(value: Date | null): string {
  if (!value) return '';
  return `${String(value.getHours()).padStart(2, '0')}:${String(value.getMinutes()).padStart(2, '0')}`;
}

function token(): string {
  return authStore.state.csrfToken ?? '';
}

async function load(): Promise<void> {
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  try {
    settings.value = await fetchNotificationSettings(fetch, signal);
    error.value = '';
    if (settings.value.quietHours) {
      quietFrom.value = clockToDate(settings.value.quietHours.startsAt);
      quietTo.value = clockToDate(settings.value.quietHours.endsAt);
      quietZone.value = settings.value.quietHours.timezone;
    }
  } catch {
    if (signal.aborted) return;
    error.value = 'Unable to load your notification settings.';
    return;
  } finally {
    if (!signal.aborted) loading.value = false;
  }

  try {
    devices.value = await fetchSubscriptions(fetch, signal);
  } catch {
    devices.value = [];
  }
}

async function toggle(kind: NotificationKind, channel: NotificationChannel, value: boolean): Promise<void> {
  busy.value = true;
  notice.value = '';
  try {
    settings.value = await setNotificationPreference(kind, channel, value, token());
  } catch {
    notice.value = 'That could not be changed.';
  } finally {
    busy.value = false;
  }
}

async function saveQuietHours(): Promise<void> {
  busy.value = true;
  notice.value = '';
  try {
    const from = dateToClock(quietFrom.value);
    const to = dateToClock(quietTo.value);
    settings.value = await setQuietHours(
      from && to ? { startsAt: from, endsAt: to, timezone: quietZone.value } : null,
      token(),
    );
    notice.value = from && to ? '' : 'Quiet hours are off.';
  } catch (caught) {
    notice.value = caught instanceof Error ? caught.message : 'Those quiet hours could not be saved.';
  } finally {
    busy.value = false;
  }
}

async function subscribe(): Promise<void> {
  busy.value = true;
  notice.value = '';
  try {
    devices.value = await subscribeThisDevice(deviceLabel(), token());
  } catch (caught) {
    notice.value = caught instanceof Error ? caught.message : 'This device could not be subscribed.';
  } finally {
    busy.value = false;
  }
}

async function remove(id: string): Promise<void> {
  busy.value = true;
  notice.value = '';
  try {
    devices.value = await revokeSubscription(id, token());
  } catch {
    notice.value = 'That device could not be removed.';
  } finally {
    busy.value = false;
  }
}

/** A name the person will recognise later, from what the browser is willing to say. */
function deviceLabel(): string {
  const platform = /Android/i.test(navigator.userAgent) ? 'Android'
    : /iPhone|iPad/i.test(navigator.userAgent) ? 'iPhone'
      : /Mac/i.test(navigator.userAgent) ? 'Mac'
        : /Windows/i.test(navigator.userAgent) ? 'Windows' : 'This device';
  return `${platform}, added ${new Date().toISOString().slice(0, 10)}`;
}

onMounted(load);
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <Panel class="notification-settings" data-testid="notification-settings">
    <template #header><h2>Notifications</h2></template>

    <Message severity="info" :closable="false" class="notification-settings__notice">
      Nothing is sent unless you asked for it. Every switch below starts off, and turning one off
      stops that kind of message immediately — on every device, and in any email already sent.
    </Message>

    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <Message v-else-if="notice" severity="warn" :closable="false">{{ notice }}</Message>

    <template v-if="settings">
      <div class="notification-settings__kinds">
        <div v-for="kind in kinds" :key="kind" class="notification-settings__kind">
          <div class="notification-settings__what">
            <p class="notification-settings__title">{{ kindWording[kind].title }}</p>
            <p class="notification-settings__detail">{{ kindWording[kind].detail }}</p>
          </div>
          <div class="notification-settings__channels">
            <label
              v-for="channel in (['email', 'web_push'] as NotificationChannel[])"
              :key="channel"
              class="notification-settings__channel"
            >
              <ToggleSwitch
                :model-value="enabled(kind, channel)"
                :disabled="busy"
                :aria-label="`${kindWording[kind].title} by ${channelWording[channel]}`"
                @update:model-value="(value: boolean) => toggle(kind, channel, value)"
              />
              <span>{{ channelWording[channel] }}</span>
            </label>
          </div>
        </div>
      </div>

      <section class="notification-settings__section" aria-labelledby="quiet-heading">
        <h3 id="quiet-heading">Quiet hours</h3>
        <p class="notification-settings__detail">
          Anything raised inside this window is held until it ends, rather than arriving at three in
          the morning. Nothing is dropped: it waits. Leave either time empty to turn quiet hours off.
        </p>
        <div class="notification-settings__quiet">
          <div class="notification-settings__field">
            <label for="quiet-from">From</label>
            <DatePicker input-id="quiet-from" v-model="quietFrom" time-only hour-format="24" />
          </div>
          <div class="notification-settings__field">
            <label for="quiet-to">Until</label>
            <DatePicker input-id="quiet-to" v-model="quietTo" time-only hour-format="24" />
          </div>
          <div class="notification-settings__field">
            <label for="quiet-zone">In</label>
            <Select
              input-id="quiet-zone"
              v-model="quietZone"
              :options="zones"
              option-label="label"
              option-value="value"
              aria-label="The timezone the quiet hours are in"
            />
          </div>
          <Button label="Save quiet hours" :loading="busy" @click="saveQuietHours" />
        </div>
      </section>

      <section class="notification-settings__section" aria-labelledby="devices-heading">
        <h3 id="devices-heading">Devices that can be pushed to</h3>
        <p class="notification-settings__detail">
          Only what your browser produced is stored: where to send, and the keys that let this
          product encrypt to it. Nothing about the device itself.
        </p>

        <p v-if="devices.length === 0" class="notification-settings__detail">
          No device is subscribed, so push notifications have nowhere to go.
        </p>
        <ul v-else class="notification-settings__devices">
          <li v-for="device in devices" :key="device.id">
            <span class="notification-settings__title">{{ device.label }}</span>
            <span class="notification-settings__detail">
              Added {{ formatDateTime(device.createdAt) }}<template v-if="device.lastUsedAt">,
                last used {{ formatDateTime(device.lastUsedAt) }}</template>
            </span>
            <Button
              type="button" size="small" severity="danger" text label="Remove"
              :disabled="busy"
              :aria-label="`Remove ${device.label}`"
              @click="remove(device.id)"
            />
          </li>
        </ul>

        <Button
          v-if="pushIsAvailable()"
          label="Subscribe this device"
          severity="secondary"
          :loading="busy"
          @click="subscribe"
        />
        <p v-else class="notification-settings__detail">
          This browser cannot receive push notifications. On an iPhone, add Market Lens to your home
          screen first.
        </p>
      </section>
    </template>
  </Panel>
</template>

<style scoped>
.notification-settings__notice {
  max-width: 70ch;
}

.notification-settings__kinds {
  display: grid;
  gap: 1rem;
  margin: 1.5rem 0;
}

.notification-settings__kind {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  padding: 1rem;
  border-radius: 0.75rem;
  background: color-mix(in srgb, currentColor 5%, transparent);
}

.notification-settings__what {
  flex: 1 1 18rem;
  min-width: 0;
}

.notification-settings__title {
  margin: 0;
  font-weight: 600;
}

.notification-settings__detail {
  margin: 0.25rem 0 0;
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
  max-width: 60ch;
}

.notification-settings__channels {
  display: flex;
  gap: 1.25rem;
  flex: 0 0 auto;
}

.notification-settings__channel {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.875rem;
}

.notification-settings__section {
  margin-top: 2rem;
}

.notification-settings__quiet {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 1rem;
  margin-top: 1rem;
}

.notification-settings__field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.notification-settings__field label {
  font-size: 0.875rem;
  font-weight: 600;
}

.notification-settings__devices {
  list-style: none;
  margin: 1rem 0;
  padding: 0;
  display: grid;
  gap: 0.75rem;
}

.notification-settings__devices li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem 1rem;
  padding: 0.875rem;
  border-radius: 0.75rem;
  background: color-mix(in srgb, currentColor 5%, transparent);
}
</style>
