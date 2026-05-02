import axios, { type AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { useAuthStore } from '@/store/auth'

export class ApiError extends Error {
  status: number
  fields?: Record<string, string>

  constructor(message: string, status: number, fields?: Record<string, string>) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.fields = fields
  }
}

const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080/api/v1'

const api = axios.create({
  baseURL: BASE_URL,
  headers: { 'Content-Type': 'application/json' },
})

api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

let isRefreshing = false
let failedQueue: Array<{ resolve: (token: string) => void; reject: (err: unknown) => void }> = []

function processQueue(error: unknown, token: string | null) {
  failedQueue.forEach(({ resolve, reject }) => {
    if (error) reject(error)
    else resolve(token!)
  })
  failedQueue = []
}

type ErrorResponse = { error: string; fields?: Record<string, string> }

api.interceptors.response.use(
  (response) => {
    const d = response.data
    return d && typeof d === 'object' && 'data' in d ? d.data : d
  },
  async (error: AxiosError<ErrorResponse>) => {
    const originalRequest = error.config as InternalAxiosRequestConfig & { _retry?: boolean }
    const status = error.response?.status
    const isRefreshEndpoint = originalRequest?.url?.includes('/auth/refresh')

    if (status === 401 && !originalRequest._retry && !isRefreshEndpoint) {
      if (isRefreshing) {
        return new Promise((resolve, reject) => {
          failedQueue.push({ resolve, reject })
        }).then((token) => {
          originalRequest.headers.Authorization = `Bearer ${token}`
          return api(originalRequest)
        })
      }

      originalRequest._retry = true
      isRefreshing = true

      const refreshToken = localStorage.getItem('pt_refresh_token')
      if (!refreshToken) {
        isRefreshing = false
        useAuthStore.getState().clearAuth()
        window.location.href = '/auth/login'
        return Promise.reject(new ApiError('Session expired', 401))
      }

      try {
        const res = await axios.post(`${BASE_URL}/auth/refresh`, {
          refresh_token: refreshToken,
        })
        const payload = res.data?.data ?? res.data
        const newToken: string = payload.access_token
        const newRefresh: string | undefined = payload.refresh_token
        useAuthStore.getState().updateToken(newToken)
        if (newRefresh) localStorage.setItem('pt_refresh_token', newRefresh)
        processQueue(null, newToken)
        originalRequest.headers.Authorization = `Bearer ${newToken}`
        return api(originalRequest)
      } catch (refreshError) {
        processQueue(refreshError, null)
        useAuthStore.getState().clearAuth()
        localStorage.removeItem('pt_refresh_token')
        window.location.href = '/auth/login'
        return Promise.reject(new ApiError('Session expired', 401))
      } finally {
        isRefreshing = false
      }
    }

    const message = error.response?.data?.error ?? error.message ?? 'Something went wrong'
    const fields = error.response?.data?.fields
    throw new ApiError(message, status ?? 0, fields)
  },
)

export default api
