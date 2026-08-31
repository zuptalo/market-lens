import { authStore } from '@/stores/auth';

export function useAuth() {
  return authStore;
}

export function bindAuthConnectivity(): () => void {
  const online = () => authStore.setOnline(true);
  const offline = () => authStore.setOnline(false);
  window.addEventListener('online', online);
  window.addEventListener('offline', offline);
  authStore.setOnline(navigator.onLine);
  return () => {
    window.removeEventListener('online', online);
    window.removeEventListener('offline', offline);
  };
}
