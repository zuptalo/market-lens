<script setup lang="ts">
import type { Session } from '@/types/auth';

defineProps<{ sessions: Session[]; busy?: boolean }>();
defineEmits<{ revoke: [sessionID: string]; revokeAll: []; logout: [] }>();
</script>

<template>
  <section class="session-list" aria-labelledby="sessions-heading">
    <div class="section-heading">
      <div>
        <p class="eyebrow">Security</p>
        <h2 id="sessions-heading">Signed-in devices</h2>
      </div>
      <button type="button" :disabled="busy" @click="$emit('revokeAll')">Sign out all devices</button>
    </div>
    <p v-if="sessions.length === 0" class="empty-state">No sessions are available.</p>
    <ul v-else class="sessions">
      <li v-for="session in sessions" :key="session.id">
        <div>
          <strong>{{ session.deviceLabel }}</strong>
          <span v-if="session.current" class="current-session">Current session</span>
          <p>Last active <time :datetime="session.lastSeenAt">{{ new Date(session.lastSeenAt).toLocaleString() }}</time></p>
        </div>
        <span v-if="session.revoked">Revoked</span>
        <button
          v-else
          type="button"
          :disabled="busy"
          :aria-label="`Revoke ${session.deviceLabel}`"
          @click="$emit('revoke', session.id)"
        >Revoke</button>
      </li>
    </ul>
    <button type="button" :disabled="busy" @click="$emit('logout')">Sign out this device</button>
  </section>
</template>
