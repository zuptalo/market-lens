<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Button from 'primevue/button';
import Panel from 'primevue/panel';
import Tag from 'primevue/tag';
import type { Session } from '@/types/auth';

defineProps<{ sessions: Session[]; busy?: boolean }>();
defineEmits<{ revoke: [sessionID: string]; revokeAll: []; logout: [] }>();
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
    <div v-else class="data-scroll">
    <DataTable
      :value="sessions"
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
          <span v-if="data.revoked">Revoked</span>
          <Button
            v-else
            type="button" size="small" severity="danger" label="Revoke"
            :disabled="busy" :aria-label="`Revoke ${data.deviceLabel}`"
            @click="$emit('revoke', data.id)" />
        </template>
      </Column>
    </DataTable>
    </div>

    <template #footer>
      <Button
        type="button" severity="secondary" label="Sign out this device"
        :disabled="busy" @click="$emit('logout')" />
    </template>
  </Panel>
</template>
