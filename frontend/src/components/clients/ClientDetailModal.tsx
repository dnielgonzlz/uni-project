import { useState } from 'react'
import { CreditCard, Package } from 'lucide-react'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import AvailabilityGrid from '@/components/availability/AvailabilityGrid'
import SessionCard from '@/components/sessions/SessionCard'
import { useClientPreferencesForCoach, useNextSessionForClient } from '@/hooks/useClientDetail'
import {
  useClientSubscription,
  useSubscriptionPlans,
  useAssignPlan,
  useCancelSubscription,
  useRequestPlanChange,
} from '@/hooks/useSubscription'

interface Props {
  clientId: string | null
  clientName: string
  onClose: () => void
}

function SubscriptionSection({ clientId }: { clientId: string }) {
  const { data: sub, isLoading } = useClientSubscription(clientId)
  const { data: plans } = useSubscriptionPlans()
  const assignPlan = useAssignPlan()
  const cancelSub = useCancelSubscription()
  const requestChange = useRequestPlanChange()
  const [selectedPlanId, setSelectedPlanId] = useState('')
  const [newPlanId, setNewPlanId] = useState('')

  const activePlans = plans?.filter((p) => p.active) ?? []

  if (isLoading) return <Skeleton className="h-16 rounded-lg" />

  if (!sub) {
    return (
      <div className="rounded-lg border border-dashed border-slate-200 bg-slate-50 px-4 py-4 space-y-3">
        <p className="text-sm text-slate-500">No subscription assigned.</p>
        {activePlans.length > 0 && (
          <div className="flex items-center gap-2">
            <Select value={selectedPlanId} onValueChange={(v) => v !== null && setSelectedPlanId(v)}>
              <SelectTrigger className="w-48 h-8 text-xs">
                <SelectValue placeholder="Select plan…" />
              </SelectTrigger>
              <SelectContent>
                {activePlans.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name} · {p.sessions_included} sessions · £{(p.amount_pence / 100).toFixed(2)}/mo
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              size="sm"
              className="h-8 text-xs"
              disabled={!selectedPlanId || assignPlan.isPending}
              onClick={() => assignPlan.mutate({ clientId, planId: selectedPlanId })}
            >
              {assignPlan.isPending ? 'Assigning…' : 'Assign plan'}
            </Button>
          </div>
        )}
      </div>
    )
  }

  const otherPlans = activePlans.filter((p) => p.id !== sub.plan_id)

  return (
    <div className="rounded-lg border border-slate-200 bg-white px-4 py-3 space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium text-slate-900">{sub.plan_name}</p>
            <Badge
              variant="outline"
              className={sub.status === 'active' ? 'border-emerald-300 text-emerald-700' : 'text-slate-400'}
            >
              {sub.status}
            </Badge>
          </div>
          <p className="text-xs text-slate-500 mt-0.5">
            {sub.sessions_balance} session{sub.sessions_balance !== 1 ? 's' : ''} remaining · {sub.sessions_included} included/month
          </p>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="text-xs text-red-500 hover:text-red-700 hover:bg-red-50"
          disabled={cancelSub.isPending}
          onClick={() => cancelSub.mutate(clientId)}
        >
          Cancel
        </Button>
      </div>

      {otherPlans.length > 0 && (
        <div className="flex items-center gap-2 pt-1 border-t border-slate-100">
          <span className="text-xs text-slate-500">Request plan change:</span>
          <Select value={newPlanId} onValueChange={(v) => v !== null && setNewPlanId(v)}>
            <SelectTrigger className="w-40 h-7 text-xs">
              <SelectValue placeholder="New plan…" />
            </SelectTrigger>
            <SelectContent>
              {otherPlans.map((p) => (
                <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            disabled={!newPlanId || requestChange.isPending}
            onClick={() => {
              requestChange.mutate({ clientId, newPlanId })
              setNewPlanId('')
            }}
          >
            {requestChange.isPending ? 'Requesting…' : 'Request'}
          </Button>
        </div>
      )}
    </div>
  )
}

export default function ClientDetailModal({ clientId, clientName, onClose }: Props) {
  const { data: windows, isLoading: loadingWindows } = useClientPreferencesForCoach(clientId)
  const nextSession = useNextSessionForClient(clientId)

  return (
    <Dialog open={!!clientId} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-w-2xl min-w-0 overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{clientName}</DialogTitle>
        </DialogHeader>

        <div className="space-y-6">
          {/* Subscription */}
          <section>
            <h3 className="text-sm font-semibold text-slate-700 mb-2 flex items-center gap-1.5">
              <Package className="h-3.5 w-3.5" />
              Subscription
            </h3>
            {clientId && <SubscriptionSection clientId={clientId} />}
          </section>

          {/* Next session */}
          <section>
            <h3 className="text-sm font-semibold text-slate-700 mb-2 flex items-center gap-1.5">
              <CreditCard className="h-3.5 w-3.5" />
              Next Session
            </h3>
            {nextSession ? (
              <SessionCard session={nextSession} />
            ) : (
              <p className="text-sm text-slate-400">No upcoming sessions confirmed.</p>
            )}
          </section>

          {/* Availability windows */}
          <section>
            <h3 className="text-sm font-semibold text-slate-700 mb-2">Saved Availability</h3>
            {loadingWindows ? (
              <Skeleton className="h-48 w-full" />
            ) : windows && windows.length > 0 ? (
              <AvailabilityGrid
                value={windows}
                onChange={() => {}}
                readOnly
              />
            ) : (
              <p className="text-sm text-slate-400">
                No availability saved yet — waiting for the client's WhatsApp reply.
              </p>
            )}
          </section>
        </div>
      </DialogContent>
    </Dialog>
  )
}
