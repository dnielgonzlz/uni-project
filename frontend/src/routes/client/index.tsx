import { createFileRoute, Link } from '@tanstack/react-router'
import { isAfter, parseISO, format } from 'date-fns'
import { CalendarDays, Clock, Package } from 'lucide-react'
import { useSessions } from '@/hooks/useSessions'
import { useClientPreferences } from '@/hooks/useAvailability'
import { useMySubscription } from '@/hooks/useSubscription'
import { useAuthStore } from '@/store/auth'
import SessionCard from '@/components/sessions/SessionCard'
import { Skeleton } from '@/components/ui/skeleton'
import { DAYS_OF_WEEK } from '@/lib/utils'

export const Route = createFileRoute('/client/')({
  component: ClientHome,
  head: () => ({ meta: [{ title: 'Overview — PT Scheduler' }] }),
})

function ClientHome() {
  const profileId = useAuthStore((s) => s.profileId) ?? ''
  const { data, isLoading } = useSessions('confirmed')
  const { data: windows, isLoading: loadingWindows } = useClientPreferences(profileId)
  const { data: sub, isLoading: loadingSub } = useMySubscription()
  const now = new Date()

  const upcoming = data
    ?.filter((s) => isAfter(parseISO(s.starts_at), now))
    .sort((a, b) => parseISO(a.starts_at).getTime() - parseISO(b.starts_at).getTime())

  const nextSession = upcoming?.[0] ?? null
  const remainingSessions = upcoming?.slice(1, 5) ?? []

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">Overview</h1>
        <p className="text-sm text-slate-500 mt-1">Your upcoming sessions and saved availability.</p>
      </div>

      {/* Session balance */}
      <section className="bg-white rounded-xl border border-slate-200 p-5">
        <h2 className="text-sm font-semibold text-slate-700 mb-3 flex items-center gap-2">
          <Package className="h-4 w-4" />
          Your Plan
        </h2>
        {loadingSub ? (
          <Skeleton className="h-10 rounded-lg" />
        ) : sub ? (
          <div className="flex items-center gap-4">
            <div className="bg-primary/10 rounded-lg px-4 py-3 text-center min-w-[64px]">
              <p className="text-2xl font-bold text-primary leading-none">{sub.sessions_balance}</p>
              <p className="text-xs text-primary/70 mt-0.5">sessions left</p>
            </div>
            <div>
              <p className="font-medium text-slate-900">{sub.plan_name}</p>
              {sub.current_period_end && (
                <p className="text-xs text-slate-400 mt-0.5">
                  Renews {format(parseISO(sub.current_period_end), 'd MMM yyyy')}
                </p>
              )}
            </div>
          </div>
        ) : (
          <p className="text-sm text-slate-400">No active plan — your coach will set one up for you.</p>
        )}
      </section>

      {/* Next session highlight */}
      <section className="bg-white rounded-xl border border-slate-200 p-5">
        <h2 className="text-sm font-semibold text-slate-700 mb-3 flex items-center gap-2">
          <Clock className="h-4 w-4" />
          Next Session
        </h2>
        {isLoading ? (
          <Skeleton className="h-14 rounded-lg" />
        ) : nextSession ? (
          <div className="flex items-center gap-3">
            <div className="bg-primary/10 rounded-lg px-4 py-3 text-center min-w-[72px]">
              <p className="text-xs text-primary font-medium uppercase tracking-wide">
                {format(parseISO(nextSession.starts_at), 'EEE')}
              </p>
              <p className="text-2xl font-bold text-primary leading-none">
                {format(parseISO(nextSession.starts_at), 'd')}
              </p>
              <p className="text-xs text-primary/70">
                {format(parseISO(nextSession.starts_at), 'MMM')}
              </p>
            </div>
            <div>
              <p className="font-medium text-slate-900">
                {format(parseISO(nextSession.starts_at), 'HH:mm')} – {format(parseISO(nextSession.ends_at), 'HH:mm')}
              </p>
              <p className="text-sm text-slate-500">Personal training session</p>
            </div>
          </div>
        ) : (
          <div className="flex items-center gap-3 py-2">
            <CalendarDays className="h-8 w-8 text-slate-300" />
            <p className="text-sm text-slate-400">No upcoming sessions yet. Your coach will be in touch soon.</p>
          </div>
        )}
      </section>

      {/* Availability summary */}
      <section className="bg-white rounded-xl border border-slate-200 p-5">
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-sm font-semibold text-slate-700">Your Saved Availability</h2>
          <Link
            to="/client/preferences"
            className="text-xs text-primary hover:underline"
          >
            Update preferences →
          </Link>
        </div>
        {loadingWindows ? (
          <Skeleton className="h-8 w-full rounded" />
        ) : windows && windows.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {windows.map((w, i) => (
              <span
                key={i}
                className="inline-flex items-center gap-1 bg-slate-100 text-slate-700 text-xs font-medium px-2.5 py-1 rounded-full"
              >
                {DAYS_OF_WEEK[w.day_of_week]} {w.start_time}–{w.end_time}
              </span>
            ))}
          </div>
        ) : (
          <p className="text-sm text-slate-400">
            No availability saved yet.{' '}
            <Link to="/client/preferences" className="text-primary hover:underline">
              Set your preferences
            </Link>{' '}
            so your coach can schedule the right times for you.
          </p>
        )}
      </section>

      {/* Remaining upcoming sessions */}
      {remainingSessions.length > 0 && (
        <section>
          <h2 className="text-sm font-semibold text-slate-700 mb-3">Upcoming Sessions</h2>
          <div className="space-y-3">
            {remainingSessions.map((s) => (
              <SessionCard
                key={s.id}
                session={s}
                counterpartName={s.coach_name ?? `Coach ${s.coach_id.slice(0, 8)}…`}
              />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
