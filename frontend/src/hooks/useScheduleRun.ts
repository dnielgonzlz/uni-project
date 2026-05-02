import { useMutation, useQuery } from '@tanstack/react-query'
import api from '@/lib/api'
import { queryClient } from '@/providers/QueryProvider'
import type { ScheduleRun } from '@/lib/types'

export function useScheduleRun(runId: string | null) {
  return useQuery<ScheduleRun>({
    queryKey: ['schedule-run', runId],
    queryFn: () => api.get(`/schedule-runs/${runId}`) as Promise<ScheduleRun>,
    enabled: !!runId,
  })
}

export function useTriggerRun() {
  return useMutation({
    mutationFn: (weekStart: string) =>
      api.post('/schedule-runs', { week_start: weekStart }, { timeout: 35_000 }) as unknown as Promise<ScheduleRun>,
  })
}

export function useConfirmRun() {
  return useMutation({
    mutationFn: ({ runId, excludedSessionIds = [] }: { runId: string; excludedSessionIds?: string[] }) =>
      api.post(`/schedule-runs/${runId}/confirm`, { excluded_session_ids: excludedSessionIds }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sessions'] })
      void queryClient.invalidateQueries({ queryKey: ['schedule-run'] })
    },
  })
}

export function useUpdateSession() {
  return useMutation({
    mutationFn: ({ sessionId, startsAt, endsAt }: { sessionId: string; startsAt: string; endsAt: string }) =>
      api.put(`/sessions/${sessionId}`, { starts_at: startsAt, ends_at: endsAt }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
  })
}

export function useRejectRun() {
  return useMutation({
    mutationFn: (runId: string) => api.post(`/schedule-runs/${runId}/reject`, {}),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['schedule-run'] })
    },
  })
}
