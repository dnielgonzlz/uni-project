import { useCallback, useEffect, useRef, useState } from 'react'
import { DAYS_OF_WEEK } from '@/lib/utils'
import type { TimeSlot } from '@/lib/types'

// 06:00–22:00 in 30-minute increments = 32 slots
const START_HOUR = 6
const END_HOUR = 22
const SLOT_MINUTES = 30
const SLOTS_PER_DAY = ((END_HOUR - START_HOUR) * 60) / SLOT_MINUTES // 32

function slotKey(day: number, slot: number) {
  return `${day}:${slot}`
}

function slotToTime(slot: number): string {
  const totalMins = START_HOUR * 60 + slot * SLOT_MINUTES
  const h = Math.floor(totalMins / 60).toString().padStart(2, '0')
  const m = (totalMins % 60).toString().padStart(2, '0')
  return `${h}:${m}`
}

function slotsFromValue(value: TimeSlot[]): Set<string> {
  const active = new Set<string>()
  for (const slot of value) {
    const day = slot.day_of_week
    const [sh, sm] = slot.start_time.split(':').map(Number)
    const [eh, em] = slot.end_time.split(':').map(Number)
    const startSlot = ((sh - START_HOUR) * 60 + sm) / SLOT_MINUTES
    const endSlot = ((eh - START_HOUR) * 60 + em) / SLOT_MINUTES
    for (let s = startSlot; s < endSlot; s++) {
      active.add(slotKey(day, s))
    }
  }
  return active
}

function valueFromSlots(active: Set<string>): TimeSlot[] {
  const result: TimeSlot[] = []
  for (let day = 0; day < 7; day++) {
    let rangeStart: number | null = null
    for (let s = 0; s <= SLOTS_PER_DAY; s++) {
      const isActive = active.has(slotKey(day, s))
      if (isActive && rangeStart === null) {
        rangeStart = s
      } else if (!isActive && rangeStart !== null) {
        result.push({
          day_of_week: day,
          start_time: slotToTime(rangeStart),
          end_time: slotToTime(s),
        })
        rangeStart = null
      }
    }
  }
  return result
}

interface Props {
  value: TimeSlot[]
  onChange: (slots: TimeSlot[]) => void
  readOnly?: boolean
}

export default function AvailabilityGrid({ value, onChange, readOnly = false }: Props) {
  const [active, setActive] = useState<Set<string>>(() => slotsFromValue(value))
  const dragging = useRef<{ mode: 'add' | 'remove'; startKey: string } | null>(null)

  useEffect(() => {
    setActive(slotsFromValue(value))
  }, [value])

  const toggle = useCallback((day: number, slot: number, mode?: 'add' | 'remove') => {
    if (readOnly) return
    setActive((prev) => {
      const next = new Set(prev)
      const key = slotKey(day, slot)
      const m = mode ?? (prev.has(key) ? 'remove' : 'add')
      if (m === 'add') next.add(key)
      else next.delete(key)
      onChange(valueFromSlots(next))
      return next
    })
  }, [readOnly, onChange])

  function onMouseDown(day: number, slot: number) {
    if (readOnly) return
    const key = slotKey(day, slot)
    const mode = active.has(key) ? 'remove' : 'add'
    dragging.current = { mode, startKey: key }
    toggle(day, slot, mode)
  }

  function onMouseEnter(day: number, slot: number) {
    if (!dragging.current) return
    toggle(day, slot, dragging.current.mode)
  }

  function onMouseUp() {
    dragging.current = null
  }

  // Build row labels (every 2 hours)
  const rowLabels: string[] = []
  for (let h = START_HOUR; h <= END_HOUR; h += 2) {
    rowLabels.push(`${h.toString().padStart(2, '0')}:00`)
  }

  // minmax(0,1fr) lets day columns shrink inside narrow parents (e.g. coach client modal) without overflow.
  const gridCols = 'grid-cols-[2.25rem_repeat(7,minmax(0,1fr))]'

  return (
    <div
      className="w-full min-w-0 max-w-full overflow-x-auto select-none"
      onMouseUp={onMouseUp}
      onMouseLeave={onMouseUp}
    >
      <div className="w-full min-w-0">
        {/* Day headers */}
        <div className={`grid ${gridCols} mb-1`}>
          <div />
          {DAYS_OF_WEEK.map((d) => (
            <div key={d} className="text-[10px] font-medium text-slate-500 text-center pb-1 sm:text-xs">
              {d}
            </div>
          ))}
        </div>

        {/* Time rows */}
        {Array.from({ length: SLOTS_PER_DAY }, (_, slotIdx) => {
          const mins = START_HOUR * 60 + slotIdx * SLOT_MINUTES
          const h = Math.floor(mins / 60)
          const m = mins % 60
          const showLabel = m === 0 && h % 2 === 0
          const label = showLabel ? `${h.toString().padStart(2, '0')}:00` : ''

          return (
            <div key={slotIdx} className={`grid ${gridCols}`} style={{ height: 14 }}>
              <div className="text-[9px] text-slate-400 pr-0.5 text-right leading-none -mt-1.5 sm:text-[10px]">
                {label}
              </div>
              {Array.from({ length: 7 }, (_, day) => {
                const isActive = active.has(slotKey(day, slotIdx))
                return (
                  <div
                    key={day}
                    onMouseDown={() => onMouseDown(day, slotIdx)}
                    onMouseEnter={() => onMouseEnter(day, slotIdx)}
                    className={`min-w-0 border-r border-b border-slate-100 transition-colors ${
                      readOnly ? 'cursor-default' : 'cursor-pointer'
                    } ${
                      isActive
                        ? readOnly
                          ? 'bg-primary/70'
                          : 'bg-primary/70 hover:bg-primary/80'
                        : readOnly
                          ? 'bg-slate-50'
                          : 'bg-slate-50 hover:bg-slate-200'
                    } ${slotIdx === 0 ? 'border-t' : ''} ${day === 0 ? 'border-l rounded-l' : ''} ${day === 6 ? 'rounded-r' : ''}`}
                  />
                )
              })}
            </div>
          )
        })}

        {!readOnly && (
          <p className="text-xs text-slate-400 mt-2">
            Click or drag to select time slots. Each block = 30 minutes.
          </p>
        )}
      </div>
    </div>
  )
}
