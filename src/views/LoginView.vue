<script setup lang="ts">
import { onUnmounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import OwnerAuth from '@/components/account/OwnerAuth.vue';
import EmailCodeForm from '@/components/account/EmailCodeForm.vue';
import { useAuth } from '@/composables/useAuth';

const auth = useAuth();
const route = useRoute();
const router = useRouter();
const busy = ref(false);
const error = ref<string | null>(null);
const resendIn = ref(0);
let resendTimer: ReturnType<typeof setInterval> | undefined;

// A code may only be requested once a minute, so the countdown reflects the real ceiling
// instead of inviting a request the server would silently drop.
const RESEND_SECONDS = 60;

function startResendCountdown(): void {
  resendIn.value = RESEND_SECONDS;
  clearInterval(resendTimer);
  resendTimer = setInterval(() => {
    resendIn.value = Math.max(0, resendIn.value - 1);
    if (resendIn.value === 0) clearInterval(resendTimer);
  }, 1000);
}

onUnmounted(() => clearInterval(resendTimer));

function destination(): string {
  return typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/') && !route.query.redirect.startsWith('//')
    ? route.query.redirect : '/';
}

async function submit(value: Record<string, string>): Promise<void> {
  busy.value = true;
  error.value = null;
  try {
    if (auth.state.signInStep === 'email') {
      await auth.startSignIn(value.email);
      startResendCountdown();
    } else {
      await auth.loginOwner({ email: value.email, password: value.password });
      await router.replace(destination());
    }
  } catch (failure) {
    error.value = failure instanceof Error ? failure.message : 'Authentication failed.';
  } finally {
    busy.value = false;
  }
}

async function verifyCode(value: { email: string; code: string }): Promise<void> {
  busy.value = true;
  error.value = null;
  try {
    await auth.loginMemberCode(value);
    await router.replace(destination());
  } catch (failure) {
    error.value = failure instanceof Error ? failure.message : 'Authentication failed.';
  } finally {
    busy.value = false;
  }
}

async function resend(): Promise<void> {
  busy.value = true;
  error.value = null;
  try {
    await auth.startSignIn(auth.state.signInEmail);
    startResendCountdown();
  } catch (failure) {
    error.value = failure instanceof Error ? failure.message : 'Authentication failed.';
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section class="auth-view">
    <template v-if="auth.state.signInStep === 'otp'">
      <EmailCodeForm
        :email="auth.state.signInEmail"
        :message="auth.state.signInMessage"
        :busy="busy"
        :error="error"
        :resend-in="resendIn"
        @submit="verifyCode"
        @resend="resend"
      />
      <button type="button" data-owner-password :disabled="busy" @click="auth.selectOwnerPassword()">
        Use owner password
      </button>
    </template>
    <OwnerAuth
      v-else
      :mode="auth.state.signInStep"
      :email="auth.state.signInEmail"
      :message="auth.state.signInMessage"
      :busy="busy"
      :error="error"
      @submit="submit"
      @use-owner-password="auth.selectOwnerPassword()"
    />
  </section>
</template>
