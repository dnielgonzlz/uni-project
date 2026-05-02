import { createFileRoute } from '@tanstack/react-router'
import { Link } from '@tanstack/react-router'
import { isAfter, parseISO, addDays } from 'date-fns'
import { CalendarDays, AlertCircle, Bot } from 'lucide-react'
import { useSessions, useCancelApprove, useCancelWaive } from '@/hooks/useSessions'
import { useAgentOverview } from '@/hooks/useAgentSettings'
import SessionCard from '@/components/sessions/SessionCard'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

export const Route = createFileRoute('/dashboard/')({
  component: DashboardOverview,
  head: () => ({ meta: [{ title: 'Overview — PT Scheduler' }] }),
})

function EmptyState({ icon: Icon, title, description }: {
  icon: React.ElementType; title: string; description: string
}) {
  return (
    <div className="flex flex-col items-center justify-center py-12 text-center">
      <Icon className="h-10 w-10 text-slate-300 mb-3" />
      <p className="font-medium text-slate-600">{title}</p>
      <p className="text-sm text-slate-400 mt-1">{description}</p>
    </div>
  )
}

function DashboardOverview() {
  const now = new Date()
  const { data: confirmed, isLoading: loadingConfirmed } = useSessions('confirmed')
  const { data: pending, isLoading: loadingPending } = useSessions('pending_cancellation')
  const { data: agentOverview, isLoading: loadingAgentOverview } = useAgentOverview()
  const approve = useCancelApprove()
  const waive = useCancelWaive()

  const upcoming = confirmed
    ?.filter((s) => isAfter(parseISO(s.starts_at), now) && isAfter(addDays(now, 7), parseISO(s.starts_at)))
    .sort((a, b) => parseISO(a.starts_at).getTime() - parseISO(b.starts_at).getTime())
    .slice(0, 5)

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">Overview</h1>
        <p className="text-slate-500 text-sm mt-1">Welcome back. Here's what's coming up.</p>
      </div>

      <section className="bg-white rounded-xl border border-slate-200 p-5">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="text-base font-semibold text-slate-900 flex items-center gap-2">
              <Bot className="h-4 w-4 text-slate-500" />
              AI availability collection
            </h2>
            <p className="text-sm text-slate-500 mt-1">
              Track who has been texted and who still needs to reply.
            </p>
          </div>
          <Button variant="outline" size="sm" render={<Link to="/dashboard/agent-settings" />}>
            View agent
          </Button>
        </div>

        {loadingAgentOverview ? (
          <div className="grid grid-cols-3 gap-3 mt-5">
            {[1, 2, 3].map((i) => <Skeleton key={i} className="h-16 rounded-lg" />)}
          </div>
        ) : agentOverview?.campaign_status === 'not_started' ? (
          <p className="text-sm text-slate-500 mt-5">
            No availability campaign has been sent yet. The agent will start showing counts after the first Friday prompt.
          </p>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mt-5">
            <div className="rounded-lg bg-slate-50 border border-slate-200 p-3">
              <p className="text-xs text-slate-500">Texted</p>
              <p className="text-2xl font-semibold text-slate-900">{agentOverview?.texted_count ?? 0}</p>
            </div>
            <div className="rounded-lg bg-emerald-50 border border-emerald-100 p-3">
              <p className="text-xs text-emerald-700">Replied</p>
              <p className="text-2xl font-semibold text-emerald-800">{agentOverview?.replied_count ?? 0}</p>
              {(agentOverview?.parsed_count ?? 0) > 0 && (
                <p className="text-xs text-emerald-600 mt-1">
                  {agentOverview!.parsed_count} AI-parsed
                </p>
              )}
            </div>
            <div className="rounded-lg bg-amber-50 border border-amber-100 p-3">
              <p className="text-xs text-amber-700">Waiting</p>
              <p className="text-2xl font-semibold text-amber-800">{agentOverview?.waiting_count ?? 0}</p>
            </div>
          </div>
        )}
      </section>

      {/* Pending cancellation requests */}
      {(loadingPending || (pending && pending.length > 0)) && (
        <section>
          <h2 className="text-base font-semibold text-slate-900 mb-3 flex items-center gap-2">
            <AlertCircle className="h-4 w-4 text-orange-500" />
            Pending cancellation requests
          </h2>
          {loadingPending ? (
            <div className="space-y-3">
              {[1, 2].map((i) => <Skeleton key={i} className="h-20 rounded-xl" />)}
            </div>
          ) : (
            <div className="space-y-3">
              {pending?.map((session) => (
                <SessionCard
                  key={session.id}
                  session={session}
                  actions={
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => approve.mutate(session.id)}
                        disabled={approve.isPending}
                      >
                        Approve
                      </Button>
                      <Button
                        size="sm"
                        onClick={() => waive.mutate(session.id)}
                        disabled={waive.isPending}
                      >
                        Waive & credit
                      </Button>
                    </div>
                  }
                />
              ))}
            </div>
          )}
        </section>
      )}

      {/* Upcoming confirmed sessions */}
      <section>
        <h2 className="text-base font-semibold text-slate-900 mb-3">Upcoming sessions (next 7 days)</h2>
        {loadingConfirmed ? (
          <div className="space-y-3">
            {[1, 2, 3].map((i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
          </div>
        ) : upcoming && upcoming.length > 0 ? (
          <div className="space-y-3">
            {upcoming.map((session) => (
              <SessionCard
                key={session.id}
                session={session}
                counterpartName={session.client_name ?? `Client ${session.client_id.slice(0, 8)}…`}
              />
            ))}
          </div>
        ) : (
          <EmptyState
            icon={CalendarDays}
            title="No upcoming sessions"
            description="Sessions will appear here once you've confirmed a schedule."
          />
        )}
      </section>
    </div>
  )
}
