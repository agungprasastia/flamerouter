"use client";

import { create } from "zustand";

export interface UserState {
  user: Record<string, unknown> | null;
  loading: boolean;
  error: string | null;
  setUser: (user: Record<string, unknown> | null) => void;
  clearUser: () => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
}

const useUserStore = create<UserState>((set) => ({
  user: null,
  loading: false,
  error: null,

  setUser: (user: Record<string, unknown> | null) => set({ user }),

  clearUser: () => set({ user: null }),

  setLoading: (loading: boolean) => set({ loading }),

  setError: (error: string | null) => set({ error }),
}));

export default useUserStore;
