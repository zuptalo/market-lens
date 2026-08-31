<script setup lang="ts">
import { ref } from 'vue';
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import InputText from 'primevue/inputtext';
import Button from 'primevue/button';
import Message from 'primevue/message';
import Panel from 'primevue/panel';
import type { DeliveryState, Invitation, InvitationState } from '@/types/auth';

const props = defineProps<{
  invitations: Invitation[];
  busy?: boolean;
  error?: string | null;
  message?: string | null;
}>();
const emit = defineEmits<{
  invite: [email: string];
  resend: [id: string];
  revoke: [id: string];
}>();

const email = ref('');

// Delivery is described in the operator's terms. Provider error codes and host names stay out
// of the interface so a failure is actionable without leaking how mail is configured.
const deliveryLabels: Record<DeliveryState, string> = {
  pending: 'Sending',
  sending: 'Sending',
  sent: 'Sent',
  failed: 'Not delivered',
  abandoned: 'Not delivered',
};

const stateLabels: Record<InvitationState, string> = {
  pending: 'Pending',
  accepted: 'Accepted',
  revoked: 'Revoked',
  expired: 'Expired',
};

// The table's row slot is untyped, so the lookups go through typed helpers rather than
// indexing a Record with `any`.
function stateLabel(invitation: Invitation): string {
  return stateLabels[invitation.state];
}

function deliveryLabel(invitation: Invitation): string {
  return deliveryLabels[invitation.deliveryState];
}

function submit(): void {
  if (props.busy) return;
  emit('invite', email.value);
  email.value = '';
}
</script>

<template>
  <Panel class="invitation-form" :pt="{ content: { class: 'form-grid' } }">
    <template #header><h2>Invitations</h2></template>
    <Message v-if="message" role="status" severity="info" :closable="false">{{ message }}</Message>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <form class="form-grid invitation-form__create" novalidate @submit.prevent="submit">
      <div class="form-field">
        <label for="invitation-email">Invite by email</label>
        <InputText
          id="invitation-email"
          v-model="email"
          name="email"
          type="email"
          inputmode="email"
          autocomplete="email"
          maxlength="320"
          required
          fluid
        />
      </div>
      <div class="form-actions">
        <Button type="submit" :disabled="busy" label="Send invitation" />
      </div>
    </form>

    <p v-if="invitations.length === 0" class="invitation-form__empty">
      No invitations yet. Invite someone by email to give them access.
    </p>
    <div v-else class="data-scroll">
    <DataTable
      :value="invitations"
      data-key="id"
      aria-label="Invitations"
    >
      <Column field="email" header="Email" :pt="{ bodyCell: { 'data-label': 'Email' } }">
        <template #body="{ data }">
          <span class="invitation-form__email">{{ data.email }}</span>
        </template>
      </Column>
      <Column header="Status" :pt="{ bodyCell: { 'data-label': 'Status' } }">
        <template #body="{ data }">
          <span class="invitation-form__meta">
            <span data-invitation-state>{{ stateLabel(data) }}</span>
            ·
            <span data-delivery-state>{{ deliveryLabel(data) }}</span>
            <template v-if="data.resendCount > 0"> · Resent {{ data.resendCount }}×</template>
          </span>
        </template>
      </Column>
      <Column header="Actions" :pt="{ bodyCell: { 'data-label': 'Actions' } }">
        <template #body="{ data }">
          <div v-if="data.state === 'pending'" class="invitation-form__actions">
            <Button
              type="button" data-resend size="small" severity="secondary" label="Resend"
              :disabled="busy" :aria-label="`Resend the invitation to ${data.email}`"
              @click="emit('resend', data.id)" />
            <Button
              type="button" data-revoke size="small" severity="danger" label="Revoke"
              :disabled="busy" :aria-label="`Revoke the invitation to ${data.email}`"
              @click="emit('revoke', data.id)" />
          </div>
        </template>
      </Column>
    </DataTable>
    </div>
  </Panel>
</template>
