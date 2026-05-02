import { useMutation, useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'
import api, { ApiError } from '@/lib/api'
import { queryClient } from '@/providers/QueryProvider'
import type {
  SubscriptionPlan,
  ClientSubscriptionDetail,
  ClientSubscriptionView,
  PlanChange,
} from '@/lib/types'

// --- Coach: plans ---

export function useSubscriptionPlans() {
  return useQuery<SubscriptionPlan[]>({
    queryKey: ['subscription-plans'],
    queryFn: () => api.get('/subscription-plans') as Promise<SubscriptionPlan[]>,
  })
}

export function useCreatePlan() {
  return useMutation({
    mutationFn: (body: { name: string; description?: string; sessions_included: number; amount_pence: number }) =>
      api.post('/subscription-plans', body) as Promise<SubscriptionPlan>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['subscription-plans'] })
      toast.success('Plan created.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to create plan'),
  })
}

export function useUpdatePlan() {
  return useMutation({
    mutationFn: ({ planId, body }: { planId: string; body: { name: string; description?: string; sessions_included: number } }) =>
      api.put(`/subscription-plans/${planId}`, body) as Promise<SubscriptionPlan>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['subscription-plans'] })
      toast.success('Plan updated.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to update plan'),
  })
}

export function useArchivePlan() {
  return useMutation({
    mutationFn: (planId: string) => api.delete(`/subscription-plans/${planId}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['subscription-plans'] })
      toast.success('Plan archived.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to archive plan'),
  })
}

// --- Coach: client subscriptions ---

export function useClientSubscription(clientId: string | null) {
  return useQuery<ClientSubscriptionDetail | null>({
    queryKey: ['client-subscription', clientId],
    queryFn: async () => {
      try {
        return await api.get(`/clients/${clientId}/subscription`) as ClientSubscriptionDetail
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null
        throw err
      }
    },
    enabled: !!clientId,
    retry: false,
  })
}

export function useAssignPlan() {
  return useMutation({
    mutationFn: ({ clientId, planId }: { clientId: string; planId: string }) =>
      api.post(`/clients/${clientId}/subscription`, { plan_id: planId }),
    onSuccess: (_data, { clientId }) => {
      void queryClient.invalidateQueries({ queryKey: ['client-subscription', clientId] })
      toast.success('Subscription assigned. First session credits granted.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to assign plan'),
  })
}

export function useCancelSubscription() {
  return useMutation({
    mutationFn: (clientId: string) => api.delete(`/clients/${clientId}/subscription`),
    onSuccess: (_data, clientId) => {
      void queryClient.invalidateQueries({ queryKey: ['client-subscription', clientId] })
      toast.success('Subscription cancelled.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to cancel subscription'),
  })
}

export function useRequestPlanChange() {
  return useMutation({
    mutationFn: ({ clientId, newPlanId }: { clientId: string; newPlanId: string }) =>
      api.post(`/clients/${clientId}/subscription/plan-change`, { new_plan_id: newPlanId }) as Promise<PlanChange>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['plan-changes'] })
      toast.success('Plan change requested — approve it in the billing page.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to request plan change'),
  })
}

// --- Coach: plan changes ---

export function usePendingPlanChanges() {
  return useQuery<PlanChange[]>({
    queryKey: ['plan-changes'],
    queryFn: () => api.get('/subscription-plan-changes') as Promise<PlanChange[]>,
  })
}

export function useApprovePlanChange() {
  return useMutation({
    mutationFn: (changeId: string) =>
      api.post(`/subscription-plan-changes/${changeId}/approve`, {}) as Promise<PlanChange>,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['plan-changes'] })
      void queryClient.invalidateQueries({ queryKey: ['client-subscription'] })
      toast.success('Plan change approved.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to approve plan change'),
  })
}

export function useRejectPlanChange() {
  return useMutation({
    mutationFn: (changeId: string) => api.post(`/subscription-plan-changes/${changeId}/reject`, {}),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['plan-changes'] })
      toast.success('Plan change rejected.')
    },
    onError: (err: Error) => toast.error(err.message ?? 'Failed to reject plan change'),
  })
}

// --- Client: own subscription view ---

export function useMySubscription() {
  return useQuery<ClientSubscriptionView | null>({
    queryKey: ['my-subscription'],
    queryFn: async () => {
      try {
        return await api.get('/me/subscription') as ClientSubscriptionView
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null
        throw err
      }
    },
    retry: false,
  })
}
