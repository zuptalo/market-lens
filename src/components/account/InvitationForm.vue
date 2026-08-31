<script setup lang="ts">
import { ref } from 'vue';
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

function submit(): void {
  if (props.busy) return;
  emit('invite', email.value);
  email.value = '';
}
</script>

<template>
  <section class="invitation-form">
    <h2>Invitations</h2>
    <p v-if="message" role="status">{{ message }}</p>
    <p v-if="error" role="alert">{{ error }}</p>

    <form class="invitation-form__create" novalidate @submit.prevent="submit">
      <label for="invitation-email">Invite by email</label>
      <input
        id="invitation-email"
        v-model="email"
        name="email"
        type="email"
        inputmode="email"
        autocomplete="email"
        maxlength="320"
        required
      >
      <button type="submit" :disabled="busy">Send invitation</button>
    </form>

    <p v-if="invitations.length === 0" class="invitation-form__empty">
      No invitations yet. Invite someone by email to give them access.
    </p>
    <ul v-else class="invitation-form__items">
      <li v-for="invitation in invitations" :key="invitation.id" class="invitation-form__item">
        <div class="invitation-form__identity">
          <span class="invitation-form__email">{{ invitation.email }}</span>
          <span class="invitation-form__meta">
            <span data-invitation-state>{{ stateLabels[invitation.state] }}</span>
            ·
            <span data-delivery-state>{{ deliveryLabels[invitation.deliveryState] }}</span>
            <template v-if="invitation.resendCount > 0"> · Resent {{ invitation.resendCount }}×</template>
          </span>
        </div>
        <div v-if="invitation.state === 'pending'" class="invitation-form__actions">
          <button
            type="button"
            data-resend
            :disabled="busy"
            :aria-label="`Resend the invitation to ${invitation.email}`"
            @click="emit('resend', invitation.id)"
          >
            Resend
          </button>
          <button
            type="button"
            data-revoke
            :disabled="busy"
            :aria-label="`Revoke the invitation to ${invitation.email}`"
            @click="emit('revoke', invitation.id)"
          >
            Revoke
          </button>
        </div>
      </li>
    </ul>
  </section>
</template>
