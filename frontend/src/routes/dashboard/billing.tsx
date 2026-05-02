import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { toast } from 'sonner'
import { Elements, CardElement, useStripe, useElements } from '@stripe/react-stripe-js'
import { CreditCard, Info, Package, Plus, Pencil, Archive, CheckCircle, XCircle, Zap } from 'lucide-react'
import api from '@/lib/api'
import { stripePromise } from '@/lib/stripe'
import { useCoachClients } from '@/hooks/useClients'
import {
  useSubscriptionPlans,
  useCreatePlan,
  useUpdatePlan,
  useArchivePlan,
  usePendingPlanChanges,
  useApprovePlanChange,
  useRejectPlanChange,
} from '@/hooks/useSubscription'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import type { SetupIntentResponse, CoachClientSummary, SubscriptionPlan } from '@/lib/types'

export const Route = createFileRoute('/dashboard/billing')({
  component: BillingPage,
  head: () => ({ meta: [{ title: 'Billing — PT Scheduler' }] }),
})

// ─── Card setup ──────────────────────────────────────────────────────────────

function SetupForm({ clientSecret, onSuccess }: { clientSecret: string; onSuccess: () => void }) {
  const stripe = useStripe()
  const elements = useElements()
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!stripe || !elements) return
    setLoading(true)
    try {
      const card = elements.getElement(CardElement)
      if (!card) return
      const { error } = await stripe.confirmCardSetup(clientSecret, {
        payment_method: { card },
      })
      if (error) {
        toast.error(error.message ?? 'Card setup failed.')
      } else {
        toast.success('Card saved successfully.')
        onSuccess()
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="p-3 border border-slate-200 rounded-lg bg-white">
        <CardElement options={{ style: { base: { fontSize: '14px', color: '#0f172a', '::placeholder': { color: '#94a3b8' } } } }} />
      </div>
      <Button type="submit" disabled={loading || !stripe}>
        {loading ? 'Saving card…' : 'Save card'}
      </Button>
    </form>
  )
}

function CardSetupTab() {
  const { data: clients, isLoading } = useCoachClients()
  const [selectedId, setSelectedId] = useState('')
  const [clientSecret, setClientSecret] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const verified = clients?.filter((c) => c.user.is_verified) ?? []
  const selected: CoachClientSummary | undefined = verified.find((c) => c.client.id === selectedId)

  function handleSelect(id: string) {
    setSelectedId(id)
    setClientSecret(null)
  }

  async function handleSetupIntent(e: React.FormEvent) {
    e.preventDefault()
    if (!selected) return
    setLoading(true)
    try {
      const data = await api.post<SetupIntentResponse>('/payments/setup-intent', {
        client_id: selected.client.id,
        email: selected.user.email,
        full_name: selected.user.full_name,
      }) as unknown as SetupIntentResponse
      setClientSecret(data.client_secret)
    } catch (err: unknown) {
      toast.error((err as Error).message ?? 'Failed to create payment setup')
    } finally {
      setLoading(false)
    }
  }

  if (isLoading) return <div className="space-y-3"><Skeleton className="h-8 w-full" /><Skeleton className="h-8 w-2/3" /></div>

  return !clientSecret ? (
    <form onSubmit={handleSetupIntent} className="space-y-4">
      <div className="space-y-1.5">
        <Label>Client</Label>
        {verified.length === 0 ? (
          <p className="text-sm text-slate-500">No verified clients yet.</p>
        ) : (
          <Select value={selectedId} onValueChange={handleSelect}>
            <SelectTrigger className="w-full"><SelectValue placeholder="Select a client…" /></SelectTrigger>
            <SelectContent>
              {verified.map((c) => (
                <SelectItem key={c.client.id} value={c.client.id}>
                  {c.user.full_name} <span className="text-slate-400 text-xs ml-1">{c.user.email}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
      {selected && (
        <div className="rounded-lg bg-slate-50 border border-slate-200 px-3 py-2.5 text-sm text-slate-600 space-y-0.5">
          <p><span className="font-medium">Name:</span> {selected.user.full_name}</p>
          <p><span className="font-medium">Email:</span> {selected.user.email}</p>
        </div>
      )}
      <Button type="submit" disabled={loading || !selectedId}>
        {loading ? 'Creating…' : 'Set up card payment'}
      </Button>
    </form>
  ) : (
    <div className="space-y-4">
      {selected && <p className="text-sm text-slate-600">Saving card for <span className="font-medium">{selected.user.full_name}</span></p>}
      <Elements stripe={stripePromise} options={{ clientSecret }}>
        <SetupForm clientSecret={clientSecret} onSuccess={() => { setClientSecret(null); setSelectedId('') }} />
      </Elements>
      <button type="button" onClick={() => setClientSecret(null)} className="text-xs text-slate-400 hover:text-slate-600 underline">Cancel</button>
    </div>
  )
}

// ─── Plans tab ────────────────────────────────────────────────────────────────

const EMPTY_PLAN = { name: '', description: '', sessions_included: '4', amount_pounds: '' }

function PlanDialog({
  open,
  onClose,
  editing,
}: {
  open: boolean
  onClose: () => void
  editing: SubscriptionPlan | null
}) {
  const createPlan = useCreatePlan()
  const updatePlan = useUpdatePlan()
  const [form, setForm] = useState(EMPTY_PLAN)

  // Sync form when editing changes
  useState(() => {
    if (editing) {
      setForm({
        name: editing.name,
        description: editing.description ?? '',
        sessions_included: String(editing.sessions_included),
        amount_pounds: editing ? String(editing.amount_pence / 100) : '',
      })
    } else {
      setForm(EMPTY_PLAN)
    }
  })

  function set(k: keyof typeof form) {
    return (e: React.ChangeEvent<HTMLInputElement>) => setForm((f) => ({ ...f, [k]: e.target.value }))
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (editing) {
      await updatePlan.mutateAsync({
        planId: editing.id,
        body: { name: form.name, description: form.description || undefined, sessions_included: Number(form.sessions_included) },
      })
    } else {
      await createPlan.mutateAsync({
        name: form.name,
        description: form.description || undefined,
        sessions_included: Number(form.sessions_included),
        amount_pence: Math.round(Number(form.amount_pounds) * 100),
      })
    }
    onClose()
  }

  const isPending = createPlan.isPending || updatePlan.isPending

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader><DialogTitle>{editing ? 'Edit plan' : 'Create plan'}</DialogTitle></DialogHeader>
        <form id="plan-form" onSubmit={handleSubmit} className="space-y-4 py-2">
          <div className="space-y-1.5">
            <Label htmlFor="name">Plan name</Label>
            <Input id="name" value={form.name} onChange={set('name')} placeholder="e.g. Premium" required />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="description">Description <span className="text-slate-400 text-xs">(optional)</span></Label>
            <Input id="description" value={form.description} onChange={set('description')} placeholder="e.g. 2 sessions per week" />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <Label htmlFor="sessions">Sessions / month</Label>
              <Input id="sessions" type="number" min="1" max="100" value={form.sessions_included} onChange={set('sessions_included')} required />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="amount">Price (£/month)</Label>
              <Input id="amount" type="number" min="1" step="0.01" value={form.amount_pounds} onChange={set('amount_pounds')} placeholder="e.g. 160.00" required={!editing} disabled={!!editing} />
              {editing && <p className="text-xs text-slate-400">Price can't be changed — archive and create a new plan instead.</p>}
            </div>
          </div>
        </form>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={isPending}>Cancel</Button>
          <Button type="submit" form="plan-form" disabled={isPending}>{isPending ? 'Saving…' : editing ? 'Save changes' : 'Create plan'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PlansTab() {
  const { data: plans, isLoading } = useSubscriptionPlans()
  const { data: changes, isLoading: loadingChanges } = usePendingPlanChanges()
  const archivePlan = useArchivePlan()
  const approvePlanChange = useApprovePlanChange()
  const rejectPlanChange = useRejectPlanChange()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<SubscriptionPlan | null>(null)

  function openCreate() { setEditing(null); setDialogOpen(true) }
  function openEdit(p: SubscriptionPlan) { setEditing(p); setDialogOpen(true) }

  function formatPrice(pence: number) {
    return `£${(pence / 100).toFixed(2)}/mo`
  }

  return (
    <div className="space-y-6">
      {/* Plans list */}
      <div className="bg-white rounded-xl border border-slate-200 p-5 space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Package className="h-5 w-5 text-slate-500" />
            <h2 className="text-base font-semibold text-slate-900">Subscription Plans</h2>
          </div>
          <Button size="sm" onClick={openCreate}><Plus className="h-3.5 w-3.5 mr-1" />New plan</Button>
        </div>

        {isLoading ? (
          <div className="space-y-2">{[1, 2].map((i) => <Skeleton key={i} className="h-14 rounded-lg" />)}</div>
        ) : !plans || plans.length === 0 ? (
          <div className="text-center py-8">
            <Package className="h-8 w-8 text-slate-300 mx-auto mb-2" />
            <p className="text-sm text-slate-500">No plans yet. Create your first subscription plan.</p>
          </div>
        ) : (
          <div className="space-y-2">
            {plans.map((p) => (
              <div key={p.id} className={`flex items-center justify-between rounded-lg border px-4 py-3 ${p.active ? 'border-slate-200' : 'border-slate-100 bg-slate-50 opacity-60'}`}>
                <div>
                  <div className="flex items-center gap-2">
                    <p className="font-medium text-slate-900 text-sm">{p.name}</p>
                    {!p.active && <Badge variant="outline" className="text-xs text-slate-400">Archived</Badge>}
                  </div>
                  <p className="text-xs text-slate-500 mt-0.5">
                    {p.sessions_included} sessions/month · {formatPrice(p.amount_pence)}
                    {p.description && ` · ${p.description}`}
                  </p>
                </div>
                {p.active && (
                  <div className="flex items-center gap-1">
                    <Button variant="ghost" size="icon" title="Edit" onClick={() => openEdit(p)}>
                      <Pencil className="h-3.5 w-3.5 text-slate-500" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      title="Archive"
                      onClick={() => archivePlan.mutate(p.id)}
                      disabled={archivePlan.isPending}
                    >
                      <Archive className="h-3.5 w-3.5 text-slate-500" />
                    </Button>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Pending plan changes */}
      <div className="bg-white rounded-xl border border-slate-200 p-5 space-y-4">
        <div className="flex items-center gap-2">
          <Zap className="h-5 w-5 text-slate-500" />
          <h2 className="text-base font-semibold text-slate-900">Pending Plan Changes</h2>
        </div>

        {loadingChanges ? (
          <Skeleton className="h-12 rounded-lg" />
        ) : !changes || changes.length === 0 ? (
          <p className="text-sm text-slate-400">No pending plan changes.</p>
        ) : (
          <div className="space-y-2">
            {changes.map((c) => (
              <div key={c.id} className="flex items-center justify-between rounded-lg border border-amber-100 bg-amber-50 px-4 py-3">
                <p className="text-sm text-amber-800">Plan change requested</p>
                <div className="flex items-center gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    className="border-emerald-300 text-emerald-700 hover:bg-emerald-50"
                    onClick={() => approvePlanChange.mutate(c.id)}
                    disabled={approvePlanChange.isPending}
                  >
                    <CheckCircle className="h-3.5 w-3.5 mr-1" />Approve
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    className="border-red-200 text-red-600 hover:bg-red-50"
                    onClick={() => rejectPlanChange.mutate(c.id)}
                    disabled={rejectPlanChange.isPending}
                  >
                    <XCircle className="h-3.5 w-3.5 mr-1" />Reject
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <PlanDialog open={dialogOpen} onClose={() => setDialogOpen(false)} editing={editing} />
    </div>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

function BillingPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">Billing</h1>
        <p className="text-sm text-slate-500 mt-1">Manage subscription plans and set up client card payments.</p>
      </div>

      <div className="flex items-start gap-2 bg-blue-50 border border-blue-200 rounded-lg p-3">
        <Info className="h-4 w-4 text-blue-500 shrink-0 mt-0.5" />
        <p className="text-sm text-blue-800">Direct Debit (GoCardless) is not available in this release. Stripe card payments only.</p>
      </div>

      <Tabs defaultValue="plans">
        <TabsList>
          <TabsTrigger value="plans"><Package className="h-3.5 w-3.5 mr-1.5" />Plans</TabsTrigger>
          <TabsTrigger value="card"><CreditCard className="h-3.5 w-3.5 mr-1.5" />Card Setup</TabsTrigger>
        </TabsList>

        <TabsContent value="plans" className="mt-4">
          <PlansTab />
        </TabsContent>

        <TabsContent value="card" className="mt-4">
          <div className="bg-white rounded-xl border border-slate-200 p-6 space-y-5">
            <div className="flex items-center gap-2">
              <CreditCard className="h-5 w-5 text-slate-500" />
              <h2 className="text-base font-semibold text-slate-900">Set up card payment for a client</h2>
            </div>
            <CardSetupTab />
          </div>
        </TabsContent>
      </Tabs>
    </div>
  )
}
