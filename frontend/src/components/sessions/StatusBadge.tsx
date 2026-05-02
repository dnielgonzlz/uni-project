import { Badge } from '@/components/ui/badge'
import type { SessionStatus } from '@/lib/types'

const LABELS: Record<SessionStatus, string> = {
  proposed: 'Proposed',
  confirmed: 'Confirmed',
  cancelled: 'Cancelled',
  completed: 'Completed',
  pending_cancellation: 'Pending cancellation',
}

const CLASSES: Record<SessionStatus, string> = {
  proposed: 'bg-yellow-100 text-yellow-800 border-yellow-200',
  confirmed: 'bg-green-100 text-green-800 border-green-200',
  cancelled: 'bg-red-100 text-red-800 border-red-200',
  completed: 'bg-slate-100 text-slate-700 border-slate-200',
  pending_cancellation: 'bg-orange-100 text-orange-800 border-orange-200',
}

export default function StatusBadge({ status }: { status: SessionStatus }) {
  return (
    <Badge className={`${CLASSES[status]} font-medium`} variant="outline">
      {LABELS[status]}
    </Badge>
  )
}
