import { formatDate, formatTime, isWithin24Hours } from '@/lib/utils'
import type { Session } from '@/lib/types'
import StatusBadge from './StatusBadge'
import { useState } from 'react'
import { format, parseISO } from 'date-fns'
import { AlertTriangle, Pencil } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogClose,
} from '@/components/ui/dialog'
import { useUpdateSession } from '@/hooks/useScheduleRun'
import { useCancelSession } from '@/hooks/useSessions'
import { toast } from 'sonner'

interface Props {
  session: Session
  counterpartName?: string
  actions?: React.ReactNode
  /** When true, show an edit button (coach-side only) */
  editable?: boolean
}

type DialogView = 'edit' | 'cancel'

function toDatetimeLocal(iso: string): string {
  return format(parseISO(iso), "yyyy-MM-dd'T'HH:mm")
}

function fromDatetimeLocal(local: string, referenceIso: string): string {
  const parsed = new Date(local + 'Z')
  if (isNaN(parsed.getTime())) return referenceIso
  return parsed.toISOString()
}

export default function SessionCard({ session, counterpartName, actions, editable = false }: Props) {
  const [open, setOpen] = useState(false)
  const [view, setView] = useState<DialogView>('edit')
  const [startsAt, setStartsAt] = useState(toDatetimeLocal(session.starts_at))
  const [endsAt, setEndsAt] = useState(toDatetimeLocal(session.ends_at))
  const [cancelReason, setCancelReason] = useState('')

  const update = useUpdateSession()
  const cancel = useCancelSession()

  function handleOpen(isOpen: boolean) {
    setOpen(isOpen)
    if (!isOpen) {
      // Reset state when dialog closes
      setView('edit')
      setStartsAt(toDatetimeLocal(session.starts_at))
      setEndsAt(toDatetimeLocal(session.ends_at))
      setCancelReason('')
    }
  }

  async function handleSave() {
    try {
      await update.mutateAsync({
        sessionId: session.id,
        startsAt: fromDatetimeLocal(startsAt, session.starts_at),
        endsAt: fromDatetimeLocal(endsAt, session.ends_at),
      })
      toast.success('Session rescheduled.')
      handleOpen(false)
    } catch (err: unknown) {
      const e = err as { message?: string }
      toast.error(e.message ?? 'Failed to update session')
    }
  }

  async function handleCancel(e: React.FormEvent) {
    e.preventDefault()
    try {
      await cancel.mutateAsync({ sessionId: session.id, reason: cancelReason })
      handleOpen(false)
    } catch (err: unknown) {
      const e = err as { message?: string }
      toast.error(e.message ?? 'Failed to cancel session')
    }
  }

  const showEdit = editable && session.status === 'confirmed'
  const within24h = isWithin24Hours(session.starts_at)

  return (
    <div className="flex items-start justify-between p-4 bg-white rounded-xl border border-slate-200 gap-4">
      <div className="min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-medium text-slate-900 text-sm">
            {formatDate(session.starts_at)} · {formatTime(session.starts_at)}–{formatTime(session.ends_at)}
          </span>
          <StatusBadge status={session.status} />
        </div>
        {counterpartName && (
          <p className="text-sm text-slate-500 mt-0.5">{counterpartName}</p>
        )}
        {session.cancellation_reason && (
          <p className="text-xs text-slate-400 mt-1 italic">"{session.cancellation_reason}"</p>
        )}
      </div>

      <div className="shrink-0 flex items-center gap-2">
        {actions}
        {showEdit && (
          <Dialog open={open} onOpenChange={handleOpen}>
            <DialogTrigger>
              <Button size="sm" variant="outline" className="gap-1.5">
                <Pencil className="h-3.5 w-3.5" />
                Edit
              </Button>
            </DialogTrigger>

            <DialogContent>
              {view === 'edit' ? (
                <>
                  <DialogHeader>
                    <DialogTitle>Edit session</DialogTitle>
                  </DialogHeader>
                  <p className="text-sm text-slate-500">
                    {counterpartName ?? 'Client'} · {formatDate(session.starts_at)} {formatTime(session.starts_at)}–{formatTime(session.ends_at)}
                  </p>

                  <div className="space-y-4 pt-2">
                    <div className="space-y-1.5">
                      <Label htmlFor="edit-starts">Start time</Label>
                      <Input
                        id="edit-starts"
                        type="datetime-local"
                        value={startsAt}
                        onChange={(e) => setStartsAt(e.target.value)}
                      />
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="edit-ends">End time</Label>
                      <Input
                        id="edit-ends"
                        type="datetime-local"
                        value={endsAt}
                        onChange={(e) => setEndsAt(e.target.value)}
                      />
                    </div>
                  </div>

                  <div className="flex items-center justify-between pt-2">
                    <div className="flex gap-2">
                      <Button onClick={handleSave} disabled={update.isPending || cancel.isPending}>
                        {update.isPending ? 'Saving…' : 'Save changes'}
                      </Button>
                      <DialogClose>
                        <Button variant="outline" disabled={update.isPending}>Close</Button>
                      </DialogClose>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-red-600 hover:text-red-700 hover:bg-red-50"
                      onClick={() => setView('cancel')}
                      disabled={update.isPending}
                    >
                      Cancel session
                    </Button>
                  </div>
                </>
              ) : (
                <>
                  <DialogHeader>
                    <DialogTitle>Cancel session</DialogTitle>
                  </DialogHeader>
                  <p className="text-sm text-slate-500">
                    {counterpartName ?? 'Client'} · {formatDate(session.starts_at)} {formatTime(session.starts_at)}–{formatTime(session.ends_at)}
                  </p>

                  {within24h && (
                    <div className="flex items-start gap-3 bg-orange-50 border border-orange-200 rounded-lg p-3">
                      <AlertTriangle className="h-4 w-4 text-orange-500 shrink-0 mt-0.5" />
                      <p className="text-sm text-orange-800">
                        This session starts within 24 hours. The client will be notified but will not automatically receive a credit.
                      </p>
                    </div>
                  )}

                  <form id="cancel-form" onSubmit={handleCancel} className="space-y-3 pt-1">
                    <div className="space-y-1.5">
                      <Label htmlFor="cancel-reason">Reason for cancellation</Label>
                      <Input
                        id="cancel-reason"
                        value={cancelReason}
                        onChange={(e) => setCancelReason(e.target.value)}
                        placeholder="e.g. Coach unavailable"
                        required
                        maxLength={500}
                      />
                    </div>
                  </form>

                  <div className="flex gap-2 pt-2">
                    <Button
                      type="submit"
                      form="cancel-form"
                      variant="destructive"
                      disabled={cancel.isPending || !cancelReason.trim()}
                    >
                      {cancel.isPending ? 'Cancelling…' : 'Confirm cancellation'}
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => setView('edit')}
                      disabled={cancel.isPending}
                    >
                      Go back
                    </Button>
                  </div>
                </>
              )}
            </DialogContent>
          </Dialog>
        )}
      </div>
    </div>
  )
}
