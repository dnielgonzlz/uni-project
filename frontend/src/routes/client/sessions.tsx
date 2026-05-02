import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { isAfter, isBefore, parseISO } from 'date-fns'
import { CalendarX } from 'lucide-react'
import { useSessions } from '@/hooks/useSessions'
import SessionCard from '@/components/sessions/SessionCard'
import CancelDialog from '@/components/sessions/CancelDialog'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { Session } from '@/lib/types'

export const Route = createFileRoute('/client/sessions')({
  component: SessionsPage,
  head: () => ({ meta: [{ title: 'My Sessions — PT Scheduler' }] }),
})

function EmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-12 text-center">
      <CalendarX className="h-9 w-9 text-slate-300 mb-2" />
      <p className="font-medium text-slate-600">{title}</p>
      <p className="text-sm text-slate-400 mt-1">{description}</p>
    </div>
  )
}

function SessionsPage() {
  const { data, isLoading } = useSessions()
  const [cancelTarget, setCancelTarget] = useState<Session | null>(null)
  const now = new Date()

  const upcoming = data?.filter(
    (s) => (s.status === 'confirmed' || s.status === 'proposed') && isAfter(parseISO(s.starts_at), now)
  ).sort((a, b) => parseISO(a.starts_at).getTime() - parseISO(b.starts_at).getTime()) ?? []

  const past = data?.filter(
    (s) => (s.status === 'confirmed' || s.status === 'completed') && isBefore(parseISO(s.starts_at), now)
  ).sort((a, b) => parseISO(b.starts_at).getTime() - parseISO(a.starts_at).getTime()) ?? []

  const cancelled = data?.filter(
    (s) => s.status === 'cancelled' || s.status === 'pending_cancellation'
  ).sort((a, b) => parseISO(b.starts_at).getTime() - parseISO(a.starts_at).getTime()) ?? []

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">My Sessions</h1>
        <p className="text-sm text-slate-500 mt-1">All your training sessions in one place.</p>
      </div>

      {isLoading ? (
        <div className="space-y-3">
          {[1, 2, 3, 4].map((i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
        </div>
      ) : (
        <Tabs defaultValue="upcoming">
          <TabsList>
            <TabsTrigger value="upcoming">Upcoming ({upcoming.length})</TabsTrigger>
            <TabsTrigger value="past">Past ({past.length})</TabsTrigger>
            <TabsTrigger value="cancelled">Cancelled ({cancelled.length})</TabsTrigger>
          </TabsList>

          <TabsContent value="upcoming" className="mt-4">
            {upcoming.length === 0 ? (
              <EmptyState title="No upcoming sessions" description="Sessions will appear here once your coach confirms a schedule." />
            ) : (
              <div className="space-y-3">
                {upcoming.map((s) => (
                  <SessionCard
                    key={s.id}
                    session={s}
                    counterpartName={s.coach_name ?? `Coach ${s.coach_id.slice(0, 8)}…`}
                    actions={
                      s.status === 'confirmed' ? (
                        <Button size="sm" variant="outline" onClick={() => setCancelTarget(s)}>
                          Cancel
                        </Button>
                      ) : undefined
                    }
                  />
                ))}
              </div>
            )}
          </TabsContent>

          <TabsContent value="past" className="mt-4">
            {past.length === 0 ? (
              <EmptyState title="No past sessions" description="Your completed sessions will appear here." />
            ) : (
              <div className="space-y-3">
                {past.map((s) => (
                  <SessionCard key={s.id} session={s} counterpartName={s.coach_name ?? `Coach ${s.coach_id.slice(0, 8)}…`} />
                ))}
              </div>
            )}
          </TabsContent>

          <TabsContent value="cancelled" className="mt-4">
            {cancelled.length === 0 ? (
              <EmptyState title="No cancelled sessions" description="Any cancelled sessions will appear here." />
            ) : (
              <div className="space-y-3">
                {cancelled.map((s) => (
                  <SessionCard key={s.id} session={s} counterpartName={s.coach_name ?? `Coach ${s.coach_id.slice(0, 8)}…`} />
                ))}
              </div>
            )}
          </TabsContent>
        </Tabs>
      )}

      {cancelTarget && (
        <CancelDialog
          session={cancelTarget}
          open={!!cancelTarget}
          onClose={() => setCancelTarget(null)}
        />
      )}
    </div>
  )
}
