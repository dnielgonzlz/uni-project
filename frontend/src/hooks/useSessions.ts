import { useMutation, useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import api from '@/lib/api'
import { queryClient } from '@/providers/QueryProvider'
import type { Session, CancelSessionResponse } from '@/lib/types'

export function useSessions(status?: string) {
  return useQuery<Session[]>({
    queryKey: ['sessions', status],
    queryFn: () => api.get(`/sessions${status ? `?status=${status}` : ''}`) as Promise<Session[]>,
  })
}

export function useCancelSession() {
  return useMutation({
    mutationFn: ({ sessionId, reason }: { sessionId: string; reason: string }) =>
      api.post(`/sessions/${sessionId}/cancel`, { reason }) as unknown as Promise<CancelSessionResponse>,
    onSuccess: (data) => {
      void queryClient.invalidateQueries({ queryKey: ['sessions'] })
      if ((data as unknown as CancelSessionResponse).within_24h_window) {
        toast.info('Cancellation request sent to your coach.')
      } else {
        toast.success('Session cancelled.')
      }
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to cancel session'),
  })
}

export function useCancelApprove() {
  return useMutation({
    mutationFn: (sessionId: string) => api.post(`/sessions/${sessionId}/cancel/approve`, {}),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sessions'] })
      toast.success('Cancellation approved.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to approve cancellation'),
  })
}

export function useCancelWaive() {
  return useMutation({
    mutationFn: (sessionId: string) => api.post(`/sessions/${sessionId}/cancel/waive`, {}),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sessions'] })
      toast.success('Cancellation approved and credit issued.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to waive cancellation'),
  })
}
