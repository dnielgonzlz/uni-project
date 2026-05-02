import { create } from 'zustand'
import type { AuthUser } from '@/lib/types'

interface AuthState {
  accessToken: string | null
  user: AuthUser | null
  profileId: string | null
  setAuth: (token: string, user: AuthUser, profileId: string | null) => void
  updateToken: (token: string) => void
  clearAuth: () => void
}

export const useAuthStore = create<AuthState>((set) => ({
  accessToken: null,
  user: null,
  profileId: null,
  setAuth: (token, user, profileId) => set({ accessToken: token, user, profileId }),
  updateToken: (token) => set({ accessToken: token }),
  clearAuth: () => set({ accessToken: null, user: null, profileId: null }),
}))
