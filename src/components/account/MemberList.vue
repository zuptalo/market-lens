<script setup lang="ts">
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import Button from 'primevue/button';
import Message from 'primevue/message';
import Panel from 'primevue/panel';
import type { Member, MemberLoginState } from '@/types/auth';

defineProps<{ members: Member[]; busy?: boolean; error?: string | null }>();
const emit = defineEmits<{
  unlock: [memberID: string];
  setStatus: [change: { id: string; status: 'active' | 'deactivated' }];
}>();

// State is conveyed as words, never colour alone, so it survives greyscale and screen readers.
const stateLabels: Record<MemberLoginState, string> = {
  available: 'Available',
  temporarily_blocked: 'Temporarily blocked',
  administratively_locked: 'Locked',
};

function stateLabel(member: Member): string {
  return stateLabels[member.loginState];
}
</script>

<template>
  <Panel class="member-list" :pt="{ content: { class: 'form-grid' } }">
    <template #header><h2>Members</h2></template>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <p v-if="members.length === 0" class="member-list__empty">No members yet. Invite someone to get started.</p>

    <!-- responsiveLayout="stack" is what keeps a table usable on a phone: below the breakpoint
         each row becomes a labelled block rather than something to scroll sideways. -->
    <div v-else class="data-scroll">
    <DataTable
      :value="members"
      data-key="id"
      aria-label="Members"
    >
      <Column field="displayName" header="Name" :pt="{ bodyCell: { 'data-label': 'Name' } }">
        <template #body="{ data }">
          <span class="member-list__name">{{ data.displayName }}</span>
          <span class="member-list__email">{{ data.email }}</span>
        </template>
      </Column>
      <Column header="Sign-in" :pt="{ bodyCell: { 'data-label': 'Sign-in' } }">
        <template #body="{ data }">
          <span data-login-state :data-state="data.loginState">{{ stateLabel(data) }}</span>
        </template>
      </Column>
      <Column header="Account" :pt="{ bodyCell: { 'data-label': 'Account' } }">
        <template #body="{ data }">{{ data.status === 'active' ? 'Active' : 'Deactivated' }}</template>
      </Column>
      <Column field="activeSessionCount" header="Signed-in devices" :pt="{ bodyCell: { 'data-label': 'Signed-in devices' } }" />
      <Column header="Actions" :pt="{ bodyCell: { 'data-label': 'Actions' } }">
        <template #body="{ data }">
          <div class="member-list__actions">
            <Button
              v-if="data.loginState === 'administratively_locked'"
              type="button" data-unlock size="small" severity="secondary"
              :disabled="busy" :aria-label="`Unlock sign-in for ${data.email}`"
              label="Unlock sign-in" @click="emit('unlock', data.id)" />
            <Button
              v-if="data.status === 'active'"
              type="button" data-deactivate size="small" severity="danger"
              :disabled="busy" :aria-label="`Deactivate ${data.email}`"
              label="Deactivate" @click="emit('setStatus', { id: data.id, status: 'deactivated' })" />
            <Button
              v-else
              type="button" data-reactivate size="small" severity="secondary"
              :disabled="busy" :aria-label="`Reactivate ${data.email}`"
              label="Reactivate" @click="emit('setStatus', { id: data.id, status: 'active' })" />
          </div>
        </template>
      </Column>
    </DataTable>
    </div>
  </Panel>
</template>
