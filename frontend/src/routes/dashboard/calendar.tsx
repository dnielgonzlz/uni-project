import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import {
  format,
  parseISO,
  startOfWeek,
  endOfWeek,
  addWeeks,
  subWeeks,
  isAfter,
  isBefore,
  addDays,
  differenceInMinutes,
} from 'date-fns'
import { enGB } from 'date-fns/locale'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useSessions } from '@/hooks/useSessions'
import CalendarURLCard from '@/components/calendar/CalendarURLCard'

export const Route = createFileRoute('/dashboard/calendar')({
  component: CoachCalendarPage,
  head: () => ({ meta: [{ title: 'Calendar — PT Scheduler' }] }),
})

// ── Grid constants ────────────────────────────────────────────────────────────
const HOUR_START  = 7   // first visible hour
const HOUR_END    = 22  // last visible hour (exclusive ceiling)
const TOTAL_HOURS = HOUR_END - HOUR_START          // 15
const HOUR_PX     = 40                             // px per hour → 15 × 40 = 600 px total
const GRID_H      = TOTAL_HOURS * HOUR_PX
// Every rendered label & line uses: top = (hour - HOUR_START) * HOUR_PX
// Labels use -translate-y-1/2 to centre on the line, so we skip the first and
// last hours (07:00 and 22:00) — their translateY would push them outside the
// container and get clipped. The grid boundary makes those edges self-evident.
const HOURS      = Array.from({ length: TOTAL_HOURS + 1 }, (_, i) => HOUR_START + i) // 7…22
const LABEL_HOURS = HOURS.filter(h => h > HOUR_START && h < HOUR_END)                // 8…21
const DAY_NAMES = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

// ── Per-client colour (deterministic hash) ───────────────────────────────────
const PALETTE = [
  'bg-blue-100   border-blue-400   text-blue-900',
  'bg-emerald-100 border-emerald-400 text-emerald-900',
  'bg-violet-100 border-violet-400 text-violet-900',
  'bg-orange-100 border-orange-400 text-orange-900',
  'bg-pink-100   border-pink-400   text-pink-900',
  'bg-cyan-100   border-cyan-400   text-cyan-900',
]
function clientColor(id: string) {
  let h = 0
  for (let i = 0; i < id.length; i++) h = id.charCodeAt(i) + ((h << 5) - h)
  return PALETTE[Math.abs(h) % PALETTE.length]
}

// ── Pixel helpers (all in px so they stay in sync with HOUR_PX lines) ────────
function sessionTop(iso: string): number {
  const d = parseISO(iso)
  const mins = (d.getHours() - HOUR_START) * 60 + d.getMinutes()
  return Math.max(0, (mins / 60) * HOUR_PX)
}

function sessionHeight(startsAt: string, endsAt: string): number {
  const start = parseISO(startsAt)
  const end   = parseISO(endsAt)
  // Clamp end to HOUR_END so sessions beyond the grid don't overflow
  const ceilMs = new Date(start).setHours(HOUR_END, 0, 0, 0)
  const clampedEnd = end.getTime() > ceilMs ? new Date(ceilMs) : end
  const mins = Math.max(15, differenceInMinutes(clampedEnd, start))
  return (mins / 60) * HOUR_PX
}

// ── Component ─────────────────────────────────────────────────────────────────
function CoachCalendarPage() {
  const [anchor, setAnchor] = useState(() =>
    startOfWeek(new Date(), { weekStartsOn: 1 }),
  )
  const weekEnd = endOfWeek(anchor, { weekStartsOn: 1 })

  const { data: sessions, isLoading } = useSessions('confirmed')

  const weekSessions = (sessions ?? []).filter((s) => {
    const t = parseISO(s.starts_at)
    return !isBefore(t, anchor) && !isAfter(t, weekEnd)
  })

  // Bucket by Mon=0 … Sun=6
  const byDay: Record<number, typeof weekSessions> = {}
  for (const s of weekSessions) {
    const dow = parseISO(s.starts_at).getDay()
    const idx = dow === 0 ? 6 : dow - 1
    ;(byDay[idx] ??= []).push(s)
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">Calendar</h1>
        <p className="text-sm text-slate-500 mt-1">Your confirmed sessions for the week.</p>
      </div>

      {/* Week navigation */}
      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" onClick={() => setAnchor(subWeeks(anchor, 1))}>
          <ChevronLeft className="h-4 w-4" />
        </Button>
        <span className="min-w-[13rem] text-center text-sm font-medium text-slate-700">
          {format(anchor, 'd MMM', { locale: enGB })} – {format(weekEnd, 'd MMM yyyy', { locale: enGB })}
        </span>
        <Button variant="outline" size="sm" onClick={() => setAnchor(addWeeks(anchor, 1))}>
          <ChevronRight className="h-4 w-4" />
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => setAnchor(startOfWeek(new Date(), { weekStartsOn: 1 }))}
        >
          Today
        </Button>
      </div>

      {/* ── Calendar card ── */}
      <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
        {isLoading ? (
          <div className="p-6 space-y-3">
            {[1, 2, 3, 4].map((i) => <Skeleton key={i} className="h-12 rounded-lg" />)}
          </div>
        ) : (
          /*
           * Single CSS grid: 1 time-gutter column + 7 day columns.
           * Row 1 = sticky day headers.  Row 2 = the time body.
           * Using one grid means column widths are always identical in both rows
           * (no scrollbar-offset skew).
           */
          <div
            className="grid"
            style={{ gridTemplateColumns: '3.5rem repeat(7, 1fr)' }}
          >
            {/* ── ROW 1 – day headers ── */}
            {/* corner */}
            <div className="h-12 border-b border-slate-200 bg-slate-50" />
            {DAY_NAMES.map((name, i) => {
              const day     = addDays(anchor, i)
              const isToday = format(day, 'yyyy-MM-dd') === format(new Date(), 'yyyy-MM-dd')
              return (
                <div
                  key={name}
                  className="h-12 border-b border-l border-slate-200 bg-slate-50
                             flex flex-col items-center justify-center gap-0.5"
                >
                  <span className={`text-[11px] font-medium uppercase tracking-wide
                    ${isToday ? 'text-blue-500' : 'text-slate-400'}`}>
                    {name}
                  </span>
                  <span className={`text-sm font-bold leading-none
                    ${isToday ? 'text-blue-600' : 'text-slate-800'}`}>
                    {format(day, 'd')}
                  </span>
                </div>
              )
            })}

            {/* ── ROW 2 – time gutter ── */}
            {/*
             * The gutter is `position: relative`, same height as the day columns.
             * Hour labels sit at `top = (h - HOUR_START) * HOUR_PX` and are then
             * shifted -50% so their centre aligns exactly with the grid line.
             * The first label (07:00) sits at top=0 → shifted up half a line-height,
             * which is fine because it visually marks the start of the hour band.
             */}
            <div
              className="relative border-r border-slate-200"
              style={{ height: GRID_H }}
            >
              {LABEL_HOURS.map((h) => (
                <div
                  key={h}
                  className="absolute right-2 text-[10px] leading-none text-slate-400
                             -translate-y-1/2 tabular-nums"
                  style={{ top: (h - HOUR_START) * HOUR_PX }}
                >
                  {String(h).padStart(2, '0')}:00
                </div>
              ))}
            </div>

            {/* ── ROW 2 – day columns ── */}
            {DAY_NAMES.map((_, dayIdx) => {
              const daySessions = byDay[dayIdx] ?? []
              return (
                <div
                  key={dayIdx}
                  className="relative border-l border-slate-200 overflow-hidden"
                  style={{ height: GRID_H }}
                >
                  {/* Hour lines — identical formula to gutter labels */}
                  {HOURS.map((h) => (
                    <div
                      key={h}
                      className="absolute inset-x-0 border-t border-slate-100"
                      style={{ top: (h - HOUR_START) * HOUR_PX }}
                    />
                  ))}

                  {/* Session blocks — pure px so they align with the lines above */}
                  {daySessions.map((s) => (
                    <div
                      key={s.id}
                      className={`absolute left-0.5 right-0.5 rounded border-l-[3px] px-1.5
                                  overflow-hidden ${clientColor(s.client_id)}`}
                      style={{
                        top:    sessionTop(s.starts_at),
                        height: sessionHeight(s.starts_at, s.ends_at),
                      }}
                      title={`${s.client_name ?? 'Client'} · ${format(parseISO(s.starts_at), 'HH:mm')}–${format(parseISO(s.ends_at), 'HH:mm')}`}
                    >
                      <p className="text-[11px] font-semibold leading-tight truncate pt-0.5">
                        {s.client_name ?? `Client ${s.client_id.slice(0, 6)}`}
                      </p>
                      <p className="text-[10px] leading-tight opacity-70">
                        {format(parseISO(s.starts_at), 'HH:mm')}–{format(parseISO(s.ends_at), 'HH:mm')}
                      </p>
                    </div>
                  ))}
                </div>
              )
            })}
          </div>
        )}
      </div>

      <CalendarURLCard />
    </div>
  )
}
