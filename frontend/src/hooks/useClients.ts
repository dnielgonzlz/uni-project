import { useMutation, useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import api from '@/lib/api'
import { queryClient } from '@/providers/QueryProvider'
import type { CoachClientSummary, CreateCoachClientInput } from '@/lib/types'

export function useCoachClients() {
  return useQuery<CoachClientSummary[]>({
    queryKey: ['coach-clients'],
    queryFn: () => api.get('/coaches/me/clients') as Promise<CoachClientSummary[]>,
  })
}

export function useCreateCoachClient() {
  return useMutation({
    mutationFn: (body: CreateCoachClientInput) =>
      api.post('/coaches/me/clients', body) as Promise<CoachClientSummary>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['coach-clients'] })
      toast.success('Client added. Password setup email sent.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to add client'),
  })
}

export function useDeleteCoachClient() {
  return useMutation({
    mutationFn: (clientId: string) => api.delete(`/coaches/me/clients/${clientId}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['coach-clients'] })
      toast.success('Client removed.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to remove client'),
  })
}
