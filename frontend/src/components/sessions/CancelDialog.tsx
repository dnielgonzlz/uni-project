import { useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { useCancelSession } from '@/hooks/useSessions'
import { isWithin24Hours, formatDate, formatTime } from '@/lib/utils'
import type { Session } from '@/lib/types'

interface Props {
  session: Session
  open: boolean
  onClose: () => void
}

export default function CancelDialog({ session, open, onClose }: Props) {
  const [reason, setReason] = useState('')
  const cancel = useCancelSession()
  const within24h = isWithin24Hours(session.starts_at)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    await cancel.mutateAsync({ sessionId: session.id, reason })
    setReason('')
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Cancel session</DialogTitle>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <p className="text-sm text-slate-600">
            {formatDate(session.starts_at)} · {formatTime(session.starts_at)}–{formatTime(session.ends_at)}
          </p>

          {within24h && (
            <div className="flex items-start gap-3 bg-orange-50 border border-orange-200 rounded-lg p-3">
              <AlertTriangle className="h-4 w-4 text-orange-500 shrink-0 mt-0.5" />
              <p className="text-sm text-orange-800">
                This session starts in less than 24 hours. Your coach will need to review this cancellation.
                You may not receive a session credit.
              </p>
            </div>
          )}

          <form onSubmit={handleSubmit} id="cancel-form" className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="reason">Reason for cancellation</Label>
              <Input
                id="reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="e.g. Feeling unwell"
                required
                maxLength={500}
              />
            </div>
          </form>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={cancel.isPending}>
            Keep session
          </Button>
          <Button
            type="submit"
            form="cancel-form"
            variant="destructive"
            disabled={cancel.isPending || !reason.trim()}
          >
            {cancel.isPending ? 'Cancelling…' : 'Cancel session'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
