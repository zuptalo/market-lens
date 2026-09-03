<script setup lang="ts">
import { computed } from 'vue';
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Button from 'primevue/button';
import Panel from 'primevue/panel';
import Tag from 'primevue/tag';
import type { Session } from '@/types/auth';

const props = defineProps<{ sessions: Session[]; busy?: boolean }>();
defineEmits<{ revoke: [sessionID: string]; revokeAll: []; logout: [] }>();

/**
 * A session that can no longer authenticate is not a signed-in device.
 *
 * The server has always sent both expiry times; this screen used to show every session it had
 * ever created and decide "live" purely on whether somebody had revoked it. So an ordinary
 * expired session — nothing wrong with it, nobody revoked it because there was nothing to
 * revoke — sat in the list with a Revoke button, and the list grew by one every time a person
 * signed in again. Somebody auditing their devices saw ten where they had one, which is the
 * opposite of what a security screen is for.
 */
function live(session: Session, now: number): boolean {
  if (session.revoked) return false;
  return Date.parse(session.idleExpiresAt) > now && Date.parse(session.absoluteExpiresAt) > now;
}

const active = computed(() => {
  const now = Date.now();
  return props.sessions.filter((session) => live(session, now));
});

const endedCount = computed(() => props.sessions.length - active.value.length);
</script>

<template>
  <Panel class="session-list" aria-labelledby="sessions-heading" :pt="{ content: { class: 'form-grid' } }">
    <template #header>
      <div class="section-heading">
        <div>
          <p class="eyebrow">Security</p>
          <h2 id="sessions-heading">Signed-in devices</h2>
        </div>
      </div>
    </template>
    <template #icons>
      <Button
        type="button" size="small" severity="secondary" label="Sign out all devices"
        :disabled="busy" @click="$emit('revokeAll')" />
    </template>

    <p v-if="sessions.length === 0" class="empty-state">No sessions are available.</p>
    <p v-else-if="active.length === 0" class="empty-state">
      No device is currently signed in. Every session on this account has expired or been revoked.
    </p>
    <div v-else class="data-scroll">
    <DataTable
      :value="active"
      data-key="id"
      aria-label="Signed-in devices"
    >
      <Column header="Device" :pt="{ bodyCell: { 'data-label': 'Device' } }">
        <template #body="{ data }">
          <strong>{{ data.deviceLabel }}</strong>
          <Tag v-if="data.current" class="current-session" value="Current session" severity="info" />
        </template>
      </Column>
      <Column header="Last active" :pt="{ bodyCell: { 'data-label': 'Last active' } }">
        <template #body="{ data }">
          <time :datetime="data.lastSeenAt">{{ new Date(data.lastSeenAt).toLocaleString() }}</time>
        </template>
      </Column>
      <Column header="Actions" :pt="{ bodyCell: { 'data-label': 'Actions' } }">
        <template #body="{ data }">
          <Button
            type="button" size="small" severity="danger" label="Revoke"
            :disabled="busy" :aria-label="`Revoke ${data.deviceLabel}`"
            @click="$emit('revoke', data.id)" />
        </template>
      </Column>
    </DataTable>
    </div>

    <!--
      Stated rather than silently dropped: somebody who expected to see an old phone here needs
      to know it is gone because its session ended, not because the screen forgot it.
    -->
    <p v-if="endedCount > 0" class="session-list__ended">
      {{ endedCount }} earlier session{{ endedCount === 1 ? '' : 's' }} on this account
      {{ endedCount === 1 ? 'has' : 'have' }} expired or been revoked and can no longer sign in.
    </p>

    <template #footer>
      <Button
        type="button" severity="secondary" label="Sign out this device"
        :disabled="busy" @click="$emit('logout')" />
    </template>
  </Panel>
</template>
