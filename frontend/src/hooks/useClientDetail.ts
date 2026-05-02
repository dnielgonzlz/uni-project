import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { isAfter } from 'date-fns'
import api from '@/lib/api'
import type { TimeSlot, Session } from '@/lib/types'
import { useSessions } from '@/hooks/useSessions'

/**
 * Fetches a specific client's preferred availability windows.
 * Callable from the coach dashboard — uses the client's UUID directly.
 */
export function useClientPreferencesForCoach(clientId: string | null) {
  return useQuery<TimeSlot[]>({
    queryKey: ['preferences', 'client', clientId],
    queryFn: () => api.get(`/clients/${clientId}/preferences`) as Promise<TimeSlot[]>,
    enabled: !!clientId,
  })
}

/**
 * Returns the next confirmed session for a given client,
 * derived from the coach's already-fetched confirmed session list.
 */
export function useNextSessionForClient(clientId: string | null): Session | null {
  const { data: sessions } = useSessions('confirmed')
  return useMemo(() => {
    if (!sessions || !clientId) return null
    const now = new Date()
    return (
      sessions
        .filter((s) => s.client_id === clientId && isAfter(new Date(s.starts_at), now))
        .sort((a, b) => new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime())[0] ??
      null
    )
  }, [sessions, clientId])
}
