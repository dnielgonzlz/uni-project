import { useMutation, useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import api from '@/lib/api'
import { queryClient } from '@/providers/QueryProvider'
import type { AgentSettings, AgentClient, CheckTemplateResponse, AgentOverview } from '@/lib/types'

export function useAgentSettings() {
  return useQuery<AgentSettings>({
    queryKey: ['agent-settings'],
    queryFn: () => api.get('/coaches/me/agent-settings') as Promise<AgentSettings>,
  })
}

export function useUpdateAgentSettings() {
  return useMutation({
    mutationFn: (body: { enabled: boolean; template_sid?: string | null }) =>
      api.put('/coaches/me/agent-settings', body) as Promise<AgentSettings>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['agent-settings'] })
      toast.success('Agent settings saved.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to save settings'),
  })
}

export function useCheckAgentTemplate() {
  return useMutation({
    mutationFn: () =>
      api.post('/coaches/me/agent-settings/check-template', {}) as Promise<CheckTemplateResponse>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['agent-settings'] })
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to check template status'),
  })
}

export function useAgentClients() {
  return useQuery<AgentClient[]>({
    queryKey: ['agent-clients'],
    queryFn: () => api.get('/coaches/me/agent-clients') as Promise<AgentClient[]>,
  })
}

export function useAgentOverview() {
  return useQuery<AgentOverview>({
    queryKey: ['agent-overview'],
    queryFn: () => api.get('/coaches/me/agent-overview') as Promise<AgentOverview>,
  })
}

export function useSendAgentCampaignNow() {
  return useMutation({
    mutationFn: () => api.post('/coaches/me/agent-campaigns/send-now', {}) as Promise<void>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['agent-overview'] })
      toast.success('Availability prompt sent to allowed clients.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to send campaign'),
  })
}

export function useUpdateAgentClient() {
  return useMutation({
    mutationFn: ({ clientId, enabled }: { clientId: string; enabled: boolean }) =>
      api.put(`/coaches/me/agent-clients/${clientId}`, { enabled }) as Promise<AgentClient>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['agent-clients'] })
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to update client'),
  })
}
