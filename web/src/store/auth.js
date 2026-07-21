import { create } from "zustand";
import * as authApi from "../api/auth";

export const useAuth = create((set) => ({
  authenticated: false,
  loading: true,
  requireLogin: true,
  async bootstrap() {
    set({ loading: true });
    try {
      const [st, rl] = await Promise.all([
        authApi.status(),
        authApi.requireLogin().catch(() => ({ requireLogin: true })),
      ]);
      set({
        authenticated: !!st.authenticated,
        requireLogin: rl.requireLogin !== false,
        loading: false,
      });
    } catch {
      set({ authenticated: false, loading: false });
    }
  },
  async login(password) {
    await authApi.login(password);
    set({ authenticated: true });
  },
  async logout() {
    await authApi.logout();
    set({ authenticated: false });
  },
}));
