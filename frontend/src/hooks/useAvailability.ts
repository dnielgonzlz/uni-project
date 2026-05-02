import { useMutation, useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import api from '@/lib/api'
import { queryClient } from '@/providers/QueryProvider'
import type { CoachProfile, TimeSlot, UpdateCoachProfileRequest } from '@/lib/types'

export function useCoachProfile(coachId: string) {
  return useQuery<CoachProfile>({
    queryKey: ['coach', 'profile', coachId],
    queryFn: () => api.get(`/coaches/${coachId}/profile`) as Promise<CoachProfile>,
    enabled: !!coachId,
  })
}

export function useUpdateCoachProfile(coachId: string) {
  return useMutation({
    mutationFn: (req: UpdateCoachProfileRequest) =>
      api.put(`/coaches/${coachId}/profile`, req) as Promise<CoachProfile>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['coach', 'profile', coachId] })
      toast.success('Profile saved.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to save profile'),
  })
}

export function useCoachAvailability(coachId: string) {
  return useQuery<TimeSlot[]>({
    queryKey: ['availability', 'coach', coachId],
    queryFn: () => api.get(`/coaches/${coachId}/availability`) as Promise<TimeSlot[]>,
    enabled: !!coachId,
  })
}

export function useSetCoachAvailability(coachId: string) {
  return useMutation({
    mutationFn: (hours: TimeSlot[]) =>
      api.put(`/coaches/${coachId}/availability`, { hours }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['availability', 'coach', coachId] })
      toast.success('Working hours saved.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to save working hours'),
  })
}

export function useClientPreferences(clientId: string) {
  return useQuery<TimeSlot[]>({
    queryKey: ['preferences', 'client', clientId],
    queryFn: () => api.get(`/clients/${clientId}/preferences`) as Promise<TimeSlot[]>,
    enabled: !!clientId,
  })
}

export function useSetClientPreferences(clientId: string) {
  return useMutation({
    mutationFn: (windows: TimeSlot[]) =>
      api.put(`/clients/${clientId}/preferences`, { windows }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['preferences', 'client', clientId] })
      toast.success('Preferences saved.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to save preferences'),
  })
}
