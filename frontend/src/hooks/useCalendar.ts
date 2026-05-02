import { useMutation, useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import api from '@/lib/api'
import { queryClient } from '@/providers/QueryProvider'
import type { CalendarURLResponse } from '@/lib/types'

export function useCalendarURL() {
  return useQuery<CalendarURLResponse>({
    queryKey: ['calendar-url'],
    queryFn: () => api.get('/me/calendar-url') as Promise<CalendarURLResponse>,
  })
}

export function useRegenerateCalendarURL() {
  return useMutation({
    mutationFn: () => api.post('/me/calendar-url/regenerate', {}),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['calendar-url'] })
      toast.success('Calendar URL regenerated.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to regenerate URL'),
  })
}
