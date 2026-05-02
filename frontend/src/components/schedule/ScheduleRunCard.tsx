import { formatDate, formatTime } from '@/lib/utils'
import { formatDistanceToNow, parseISO } from 'date-fns'
import { enGB } from 'date-fns/locale'
import { Clock } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import type { ScheduleRun } from '@/lib/types'
import { DAYS_OF_WEEK } from '@/lib/utils'
import { useConfirmRun, useRejectRun } from '@/hooks/useScheduleRun'
import { toast } from 'sonner'

interface Props {
  run: ScheduleRun
  onConfirmed: () => void
  onRejected: () => void
}

export default function ScheduleRunCard({ run, onConfirmed, onRejected }: Props) {
  const confirm = useConfirmRun()
  const reject = useRejectRun()

  // All sessions selected by default; unchecked IDs get excluded (cancelled)
  const [excluded, setExcluded] = useState<Set<string>>(new Set())

  const sessionsByDay: Record<number, typeof run.sessions> = {}
  for (const s of run.sessions) {
    const day = parseISO(s.starts_at).getDay()
    const adjustedDay = day === 0 ? 6 : day - 1
    if (!sessionsByDay[adjustedDay]) sessionsByDay[adjustedDay] = []
    sessionsByDay[adjustedDay].push(s)
  }

  function toggleSession(id: string) {
    setExcluded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const selectedCount = run.sessions.length - excluded.size

  async function handleConfirm() {
    try {
      await confirm.mutateAsync({ runId: run.id, excludedSessionIds: [...excluded] })
      toast.success(
        excluded.size > 0
          ? `${selectedCount} session${selectedCount !== 1 ? 's' : ''} confirmed. ${excluded.size} skipped.`
          : 'Schedule confirmed. Clients will be notified.',
      )
      onConfirmed()
    } catch (err: unknown) {
      const e = err as { message?: string }
      toast.error(e.message ?? 'Failed to confirm schedule')
    }
  }

  async function handleReject() {
    try {
      await reject.mutateAsync(run.id)
      toast.info('Schedule rejected.')
      onRejected()
    } catch (err: unknown) {
      const e = err as { message?: string }
      toast.error(e.message ?? 'Failed to reject schedule')
    }
  }

  return (
    <div className="bg-white rounded-xl border border-slate-200 p-6 space-y-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="font-semibold text-slate-900">Proposed schedule</h3>
          <p className="text-sm text-slate-500">
            Week of {formatDate(run.week_start)} · {run.sessions.length} session{run.sessions.length !== 1 ? 's' : ''}
          </p>
        </div>
        <div className="flex items-center gap-1.5 text-xs text-slate-400 shrink-0">
          <Clock className="h-3.5 w-3.5" />
          <span>
            Expires {formatDistanceToNow(parseISO(run.expires_at), { addSuffix: true, locale: enGB })}
          </span>
        </div>
      </div>

      {/* Sessions grouped by day with checkboxes */}
      <div className="space-y-3">
        {Object.entries(sessionsByDay).sort(([a], [b]) => Number(a) - Number(b)).map(([dayStr, sessions]) => (
          <div key={dayStr}>
            <p className="text-xs font-semibold text-slate-500 uppercase mb-1.5">
              {DAYS_OF_WEEK[Number(dayStr)]}
            </p>
            <div className="space-y-1.5">
              {sessions.sort((a, b) => parseISO(a.starts_at).getTime() - parseISO(b.starts_at).getTime()).map((s) => {
                const isExcluded = excluded.has(s.id)
                return (
                  <label
                    key={s.id}
                    className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm cursor-pointer transition-colors ${
                      isExcluded ? 'bg-slate-100 opacity-50' : 'bg-slate-50 hover:bg-slate-100'
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={!isExcluded}
                      onChange={() => toggleSession(s.id)}
                      className="h-4 w-4 rounded border-slate-300 text-slate-900 accent-slate-800"
                    />
                    <span className={`font-medium ${isExcluded ? 'line-through text-slate-400' : 'text-slate-700'}`}>
                      {formatTime(s.starts_at)}–{formatTime(s.ends_at)}
                    </span>
                    <span className={isExcluded ? 'text-slate-400' : 'text-slate-500'}>
                      {s.client_name ?? `Client ${s.client_id.slice(0, 8)}…`}
                    </span>
                    {isExcluded && (
                      <span className="ml-auto text-xs text-orange-500 font-medium">Skipped</span>
                    )}
                  </label>
                )
              })}
            </div>
          </div>
        ))}
      </div>

      {excluded.size > 0 && (
        <p className="text-xs text-orange-600 bg-orange-50 border border-orange-200 rounded-lg px-3 py-2">
          {excluded.size} session{excluded.size !== 1 ? 's' : ''} will be skipped and cancelled — handle them manually.
        </p>
      )}

      <div className="flex gap-3 pt-2">
        <Button
          onClick={handleConfirm}
          disabled={confirm.isPending || reject.isPending || selectedCount === 0}
        >
          {confirm.isPending
            ? 'Confirming…'
            : selectedCount === run.sessions.length
              ? 'Confirm all sessions'
              : `Confirm ${selectedCount} session${selectedCount !== 1 ? 's' : ''}`}
        </Button>
        <Button
          variant="outline"
          onClick={handleReject}
          disabled={confirm.isPending || reject.isPending}
        >
          {reject.isPending ? 'Rejecting…' : 'Reject & regenerate'}
        </Button>
      </div>
    </div>
  )
}
