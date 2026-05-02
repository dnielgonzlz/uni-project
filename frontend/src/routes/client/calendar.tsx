import { createFileRoute } from '@tanstack/react-router'
import CalendarURLCard from '@/components/calendar/CalendarURLCard'

export const Route = createFileRoute('/client/calendar')({
  component: ClientCalendarPage,
  head: () => ({ meta: [{ title: 'Calendar — PT Scheduler' }] }),
})

function ClientCalendarPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">Calendar subscription</h1>
        <p className="text-sm text-slate-500 mt-1">
          Subscribe to your sessions in any calendar app using this ICS feed.
        </p>
      </div>
      <CalendarURLCard />
    </div>
  )
}
