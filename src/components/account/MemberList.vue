<script setup lang="ts">
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
  <section class="member-list">
    <h2>Members</h2>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="members.length === 0" class="member-list__empty">No members yet. Invite someone to get started.</p>

    <ul v-else class="member-list__items">
      <li v-for="member in members" :key="member.id" class="member-list__item">
        <div class="member-list__identity">
          <span class="member-list__name">{{ member.displayName }}</span>
          <span class="member-list__email">{{ member.email }}</span>
        </div>
        <dl class="member-list__meta">
          <dt>Sign-in</dt>
          <dd data-login-state :data-state="member.loginState">{{ stateLabel(member) }}</dd>
          <dt>Account</dt>
          <dd>{{ member.status === 'active' ? 'Active' : 'Deactivated' }}</dd>
          <dt>Signed-in devices</dt>
          <dd>{{ member.activeSessionCount }}</dd>
        </dl>
        <div class="member-list__actions">
          <button
            v-if="member.loginState === 'administratively_locked'"
            type="button"
            data-unlock
            :disabled="busy"
            :aria-label="`Unlock sign-in for ${member.email}`"
            @click="emit('unlock', member.id)"
          >
            Unlock sign-in
          </button>
          <button
            v-if="member.status === 'active'"
            type="button"
            data-deactivate
            :disabled="busy"
            :aria-label="`Deactivate ${member.email}`"
            @click="emit('setStatus', { id: member.id, status: 'deactivated' })"
          >
            Deactivate
          </button>
          <button
            v-else
            type="button"
            data-reactivate
            :disabled="busy"
            :aria-label="`Reactivate ${member.email}`"
            @click="emit('setStatus', { id: member.id, status: 'active' })"
          >
            Reactivate
          </button>
        </div>
      </li>
    </ul>
  </section>
</template>
