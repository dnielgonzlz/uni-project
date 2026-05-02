import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { format, nextMonday, isMonday } from 'date-fns'
import { AlertCircle, CalendarDays } from 'lucide-react'
import { toast } from 'sonner'
import { useTriggerRun } from '@/hooks/useScheduleRun'
import { useSessions, useCancelApprove, useCancelWaive } from '@/hooks/useSessions'
import ScheduleRunCard from '@/components/schedule/ScheduleRunCard'
import SolverStatusBanner from '@/components/schedule/SolverStatusBanner'
import SessionCard from '@/components/sessions/SessionCard'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { ApiError } from '@/lib/api'
import type { ScheduleRun } from '@/lib/types'
import { isAfter, parseISO, startOfWeek, endOfWeek } from 'date-fns'

export const Route = createFileRoute('/dashboard/schedule')({
  component: SchedulePage,
  head: () => ({ meta: [{ title: 'Schedule — PT Scheduler' }] }),
})

function defaultWeekStart(): string {
  const today = new Date()
  const monday = isMonday(today) ? today : nextMonday(today)
  return format(monday, 'yyyy-MM-dd')
}

function SchedulePage() {
  const [weekStart, setWeekStart] = useState(defaultWeekStart)
  const [pendingRun, setPendingRun] = useState<ScheduleRun | null>(null)
  const [infeasible, setInfeasible] = useState(false)

  const trigger = useTriggerRun()
  const { data: pending, isLoading: loadingPending } = useSessions('pending_cancellation')
  const { data: confirmed, isLoading: loadingConfirmed } = useSessions('confirmed')
  const approve = useCancelApprove()
  const waive = useCancelWaive()

  const now = new Date()
  const weekStartDate = weekStart ? parseISO(weekStart) : now
  const weekEnd = endOfWeek(weekStartDate, { weekStartsOn: 1 })
  const weekSessions = confirmed?.filter(
    (s) => isAfter(parseISO(s.starts_at), startOfWeek(weekStartDate, { weekStartsOn: 1 }))
      && !isAfter(parseISO(s.starts_at), weekEnd),
  ) ?? []

  async function handleGenerate(e: React.FormEvent) {
    e.preventDefault()
    setInfeasible(false)
    try {
      const run = await trigger.mutateAsync(weekStart) as unknown as ScheduleRun
      setPendingRun(run)
    } catch (err) {
      if (err instanceof ApiError && err.status === 422) {
        setInfeasible(true)
      } else {
        toast.error((err as Error).message ?? 'Failed to generate schedule')
      }
    }
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">Schedule</h1>
        <p className="text-sm text-slate-500 mt-1">
          Generate and confirm weekly schedules for your clients.
        </p>
      </div>

      {/* Pending cancellation requests */}
      {(loadingPending || (pending && pending.length > 0)) && (
        <section>
          <h2 className="text-base font-semibold text-slate-900 mb-3 flex items-center gap-2">
            <AlertCircle className="h-4 w-4 text-orange-500" />
            Pending cancellation requests
          </h2>
          {loadingPending ? (
            <div className="space-y-2">
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
                      <Button size="sm" variant="outline" onClick={() => approve.mutate(session.id)} disabled={approve.isPending}>
                        Approve
                      </Button>
                      <Button size="sm" onClick={() => waive.mutate(session.id)} disabled={waive.isPending}>
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

      {/* Generate new schedule form */}
      {!pendingRun && (
        <section className="bg-white rounded-xl border border-slate-200 p-6">
          <h2 className="text-base font-semibold text-slate-900 mb-4">Generate a schedule</h2>

          {infeasible && (
            <div className="flex items-start gap-3 bg-red-50 border border-red-200 rounded-lg p-4 mb-4">
              <AlertCircle className="h-5 w-5 text-red-500 shrink-0 mt-0.5" />
              <div>
                <p className="font-medium text-red-800">No feasible schedule found</p>
                <p className="text-sm text-red-700 mt-0.5">
                  Check your working hours and client session counts. There may not be enough available slots.
                </p>
              </div>
            </div>
          )}

          {trigger.isPending ? (
            <SolverStatusBanner />
          ) : (
            <form onSubmit={handleGenerate} className="flex items-end gap-4">
              <div className="space-y-1.5 flex-1">
                <Label htmlFor="week_start">Week starting (Monday)</Label>
                <Input
                  id="week_start"
                  type="date"
                  value={weekStart}
                  onChange={(e) => setWeekStart(e.target.value)}
                  required
                />
              </div>
              <Button type="submit">Generate schedule</Button>
            </form>
          )}
        </section>
      )}

      {/* Pending confirmation run */}
      {pendingRun && (
        <ScheduleRunCard
          run={pendingRun}
          onConfirmed={() => setPendingRun(null)}
          onRejected={() => setPendingRun(null)}
        />
      )}

      {/* This week's confirmed sessions */}
      <section>
        <h2 className="text-base font-semibold text-slate-900 mb-3">
          Confirmed sessions — week of {format(weekStartDate, 'dd/MM/yyyy')}
        </h2>
        {loadingConfirmed ? (
          <div className="space-y-2">
            {[1, 2, 3].map((i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
          </div>
        ) : weekSessions.length > 0 ? (
          <div className="space-y-3">
            {weekSessions
              .sort((a, b) => parseISO(a.starts_at).getTime() - parseISO(b.starts_at).getTime())
              .map((s) => (
                <SessionCard key={s.id} session={s} counterpartName={s.client_name ?? `Client ${s.client_id.slice(0, 8)}…`} editable />
              ))}
          </div>
        ) : (
          <div className="flex flex-col items-center justify-center py-10 text-center bg-white rounded-xl border border-slate-200">
            <CalendarDays className="h-9 w-9 text-slate-300 mb-2" />
            <p className="font-medium text-slate-600">No confirmed sessions for this week</p>
            <p className="text-sm text-slate-400 mt-1">Generate and confirm a schedule above.</p>
          </div>
        )}
      </section>
    </div>
  )
}
