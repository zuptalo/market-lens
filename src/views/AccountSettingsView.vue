<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import SessionList from '@/components/account/SessionList.vue';
import MemberList from '@/components/account/MemberList.vue';
import InvitationForm from '@/components/account/InvitationForm.vue';
import { useAuth } from '@/composables/useAuth';
import type { AccountStatus, Invitation, Member, Session } from '@/types/auth';

const auth = useAuth();
const router = useRouter();
const sessions = ref<Session[]>([]);
const busy = ref(false);
const error = ref<string | null>(null);
const message = ref<string | null>(null);
const members = ref<Member[]>([]);
const memberError = ref<string | null>(null);
const invitations = ref<Invitation[]>([]);
const invitationError = ref<string | null>(null);
const invitationMessage = ref<string | null>(null);
const isOwner = computed(() => auth.state.account?.role === 'owner');
let stopMemberWatch: (() => void) | undefined;

onMounted(async () => {
  await loadSessions();
  if (!isOwner.value) return;
  await Promise.all([loadMembers(), loadInvitations()]);
  // Owner-scoped member and invitation events refresh the console; it never polls.
  stopMemberWatch = auth.onMembersChanged(() => {
    void loadMembers();
    void loadInvitations();
  });
});

onUnmounted(() => stopMemberWatch?.());

async function loadMembers(): Promise<void> {
  try {
    members.value = (await auth.members()).members;
    memberError.value = null;
  } catch {
    memberError.value = 'Member administration is temporarily unavailable.';
  }
}

async function loadInvitations(): Promise<void> {
  try {
    invitations.value = (await auth.invitations()).items;
    invitationError.value = null;
  } catch {
    invitationError.value = 'Invitation administration is temporarily unavailable.';
  }
}

async function invite(email: string): Promise<void> {
  busy.value = true;
  invitationError.value = null;
  invitationMessage.value = null;
  try {
    await auth.createInvitation(email);
    await loadInvitations();
    invitationMessage.value = 'Invitation sent.';
  } catch (failure) {
    invitationError.value = failure instanceof Error ? failure.message : 'Unable to send the invitation.';
  } finally { busy.value = false; }
}

async function resendInvitation(invitationID: string): Promise<void> {
  busy.value = true;
  invitationError.value = null;
  invitationMessage.value = null;
  try {
    await auth.resendInvitation(invitationID);
    await loadInvitations();
    invitationMessage.value = 'Invitation resent.';
  } catch (failure) {
    invitationError.value = failure instanceof Error ? failure.message : 'Unable to resend the invitation.';
  } finally { busy.value = false; }
}

async function revokeInvitation(invitationID: string): Promise<void> {
  busy.value = true;
  invitationError.value = null;
  invitationMessage.value = null;
  try {
    await auth.revokeInvitation(invitationID);
    await loadInvitations();
    invitationMessage.value = 'Invitation revoked.';
  } catch (failure) {
    invitationError.value = failure instanceof Error ? failure.message : 'Unable to revoke the invitation.';
  } finally { busy.value = false; }
}

async function setMemberStatus(change: { id: string; status: AccountStatus }): Promise<void> {
  busy.value = true;
  memberError.value = null;
  try {
    await auth.setMemberStatus(change.id, change.status);
    await loadMembers();
    message.value = change.status === 'deactivated' ? 'Member deactivated.' : 'Member reactivated.';
  } catch (failure) {
    memberError.value = failure instanceof Error ? failure.message : 'Unable to change the member status.';
  } finally { busy.value = false; }
}

async function unlockMember(memberID: string): Promise<void> {
  busy.value = true;
  memberError.value = null;
  try {
    await auth.unlockMember(memberID);
    await loadMembers();
    message.value = 'Member sign-in unlocked.';
  } catch (failure) {
    memberError.value = failure instanceof Error ? failure.message : 'Unable to unlock the member.';
  } finally { busy.value = false; }
}

async function loadSessions(): Promise<void> {
  try { sessions.value = await auth.sessions(); }
  catch { error.value = 'Unable to load signed-in devices.'; }
}

async function revoke(sessionID: string): Promise<void> {
  busy.value = true;
  error.value = null;
  try {
    await auth.revokeSession(sessionID);
    sessions.value = sessions.value.map((session) => session.id === sessionID ? { ...session, revoked: true } : session);
    message.value = 'Session revoked.';
  } catch (failure) {
    error.value = failure instanceof Error ? failure.message : 'Unable to revoke the session.';
  } finally { busy.value = false; }
}

async function revokeAll(): Promise<void> {
  busy.value = true;
  try { await auth.revokeAllSessions(); await router.replace('/login'); }
  catch (failure) { error.value = failure instanceof Error ? failure.message : 'Unable to revoke sessions.'; }
  finally { busy.value = false; }
}

async function logout(): Promise<void> {
  busy.value = true;
  try { await auth.logout(); await router.replace('/login'); }
  catch (failure) { error.value = failure instanceof Error ? failure.message : 'Unable to sign out.'; }
  finally { busy.value = false; }
}
</script>

<template>
  <div class="account-view">
    <header class="page-intro">
      <p class="eyebrow">Your account</p>
      <h1>Account settings</h1>
      <p>{{ auth.state.account?.displayName }} · {{ auth.state.account?.email }}</p>
    </header>
    <p v-if="message" role="status">{{ message }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <SessionList :sessions="sessions" :busy="busy" @revoke="revoke" @revoke-all="revokeAll" @logout="logout" />
    <MemberList
      v-if="isOwner"
      :members="members"
      :busy="busy"
      :error="memberError"
      @unlock="unlockMember"
      @set-status="setMemberStatus"
    />
    <InvitationForm
      v-if="isOwner"
      :invitations="invitations"
      :busy="busy"
      :error="invitationError"
      :message="invitationMessage"
      @invite="invite"
      @resend="resendInvitation"
      @revoke="revokeInvitation"
    />
  </div>
</template>
