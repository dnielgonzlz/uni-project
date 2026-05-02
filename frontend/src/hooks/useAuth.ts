import api, { ApiError } from '@/lib/api'
import { useAuthStore } from '@/store/auth'
import type { AuthUser } from '@/lib/types'

type AuthResponse = {
  access_token: string
  refresh_token: string
}

type MeExportResponse = {
  user: {
    id: string
    email: string
    role: 'coach' | 'client'
    full_name: string
  }
  profile?: {
    id?: string
  }
}

export async function fetchCurrentUser(): Promise<{ user: AuthUser; profileId: string | null }> {
  console.log('[fetchCurrentUser] calling /me/export')
  const data = await api.get('/me/export')
  console.log('[fetchCurrentUser] raw response', data)

  const payload = data as unknown as MeExportResponse
  const user: AuthUser = {
    id: payload.user.id,
    email: payload.user.email,
    role: payload.user.role,
    full_name: payload.user.full_name,
  }
  const profileId = payload.profile?.id ?? null

  return { user, profileId }
}

async function hydrateAuthFromTokens(payload: AuthResponse): Promise<AuthUser> {
  const { setAuth, clearAuth, updateToken } = useAuthStore.getState()

  localStorage.setItem('pt_refresh_token', payload.refresh_token)
  updateToken(payload.access_token)

  try {
    const { user, profileId } = await fetchCurrentUser()
    setAuth(payload.access_token, user, profileId)
    return user
  } catch (error) {
    clearAuth()
    localStorage.removeItem('pt_refresh_token')
    throw error
  }
}

export async function ensureAuthState(): Promise<AuthUser | null> {
  const {
    accessToken, user, profileId, setAuth, clearAuth, updateToken,
  } = useAuthStore.getState()
  const refreshToken = localStorage.getItem('pt_refresh_token')

  if (accessToken && user && profileId) return user

  if (accessToken) {
    try {
      const current = await fetchCurrentUser()
      setAuth(accessToken, current.user, current.profileId)
      return current.user
    } catch {
      if (!refreshToken) {
        clearAuth()
        localStorage.removeItem('pt_refresh_token')
        return null
      }
    }
  }

  if (!refreshToken) return null

  try {
    const data = await api.post<AuthResponse>('/auth/refresh', { refresh_token: refreshToken })
    const payload = data as unknown as AuthResponse

    updateToken(payload.access_token)
    if (payload.refresh_token) {
      localStorage.setItem('pt_refresh_token', payload.refresh_token)
    }

    const current = await fetchCurrentUser()
    setAuth(payload.access_token, current.user, current.profileId)
    return current.user
  } catch {
    clearAuth()
    localStorage.removeItem('pt_refresh_token')
    return null
  }
}

export function useAuth() {
  const { user, accessToken, profileId, clearAuth } = useAuthStore()

  async function login(email: string, password: string) {
    console.log('[login] starting')
    const data = await api.post<AuthResponse>('/auth/login', { email, password })

    const payload = data as unknown as AuthResponse
    console.log('[login] /auth/login response', payload)
    const authUser = await hydrateAuthFromTokens(payload)
    console.log('[login] hydrated auth, returning user', authUser)
    return authUser
  }

  async function register(body: {
    email: string
    password: string
    full_name: string
    role: 'coach' | 'client'
    timezone?: string
    business_name?: string
    coach_id?: string
    sessions_per_month?: number
  }) {
    console.log('[register] starting with body', body)
    const data = await api.post<AuthResponse>('/auth/register', { timezone: 'Europe/London', ...body })

    const payload = data as unknown as AuthResponse
    console.log('[register] /auth/register response', payload)
    const authUser = await hydrateAuthFromTokens(payload)
    console.log('[register] hydrated auth, returning user', authUser)
    return authUser
  }

  async function logout() {
    try {
      const refreshToken = localStorage.getItem('pt_refresh_token')
      if (refreshToken) {
        await api.post('/auth/logout', { refresh_token: refreshToken })
      }
    } catch {
      // ignore errors on logout
    } finally {
      clearAuth()
      localStorage.removeItem('pt_refresh_token')
    }
  }

  return {
    user,
    accessToken,
    profileId,
    isAuthenticated: !!accessToken && !!user,
    login,
    register,
    logout,
    ApiError,
  }
}
