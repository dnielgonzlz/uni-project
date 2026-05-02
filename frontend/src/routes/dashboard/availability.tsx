import { createFileRoute } from '@tanstack/react-router'
import { useState, useEffect } from 'react'
import { useAuthStore } from '@/store/auth'
import {
  useCoachAvailability,
  useSetCoachAvailability,
  useCoachProfile,
  useUpdateCoachProfile,
} from '@/hooks/useAvailability'
import AvailabilityGrid from '@/components/availability/AvailabilityGrid'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { TimeSlot } from '@/lib/types'

export const Route = createFileRoute('/dashboard/availability')({
  component: AvailabilityPage,
  head: () => ({ meta: [{ title: 'Availability — PT Scheduler' }] }),
})

function AvailabilityPage() {
  const profileId = useAuthStore((s) => s.profileId) ?? ''

  const { data: availData, isLoading: availLoading } = useCoachAvailability(profileId)
  const { data: profile, isLoading: profileLoading } = useCoachProfile(profileId)
  const saveAvail = useSetCoachAvailability(profileId)
  const saveProfile = useUpdateCoachProfile(profileId)

  const [slots, setSlots] = useState<TimeSlot[]>([])
  const [maxSessions, setMaxSessions] = useState(4)

  useEffect(() => {
    if (availData) setSlots(availData)
  }, [availData])

  useEffect(() => {
    if (profile) setMaxSessions(profile.coach.max_sessions_per_day ?? 4)
  }, [profile])

  const handleSave = () => {
    saveAvail.mutate(slots)
    if (profile) {
      saveProfile.mutate({
        full_name: profile.user.full_name,
        business_name: profile.coach.business_name,
        phone: profile.user.phone,
        timezone: profile.user.timezone,
        max_sessions_per_day: maxSessions,
      })
    }
  }

  const isLoading = availLoading || profileLoading
  const isPending = saveAvail.isPending || saveProfile.isPending

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">Working hours</h1>
        <p className="text-sm text-slate-500 mt-1">
          Set the times you're available to train clients. The scheduler will only book sessions within these hours.
        </p>
      </div>

      <div className="bg-white rounded-xl border border-slate-200 p-6">
        {isLoading ? (
          <Skeleton className="h-64 w-full rounded-lg" />
        ) : (
          <AvailabilityGrid value={slots} onChange={setSlots} />
        )}
      </div>

      <div className="bg-white rounded-xl border border-slate-200 p-6">
        <h2 className="text-base font-medium text-slate-900 mb-1">Daily session limit</h2>
        <p className="text-sm text-slate-500 mb-4">
          Maximum number of sessions you'll accept in a single day (2–8).
        </p>
        {isLoading ? (
          <Skeleton className="h-10 w-32 rounded-lg" />
        ) : (
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => setMaxSessions((v) => Math.max(2, v - 1))}
              disabled={maxSessions <= 2}
              className="w-9 h-9 rounded-lg border border-slate-200 text-slate-700 text-lg font-medium hover:bg-slate-50 disabled:opacity-40 disabled:cursor-not-allowed"
            >
              −
            </button>
            <span className="w-8 text-center text-lg font-semibold text-slate-900 tabular-nums">
              {maxSessions}
            </span>
            <button
              type="button"
              onClick={() => setMaxSessions((v) => Math.min(8, v + 1))}
              disabled={maxSessions >= 8}
              className="w-9 h-9 rounded-lg border border-slate-200 text-slate-700 text-lg font-medium hover:bg-slate-50 disabled:opacity-40 disabled:cursor-not-allowed"
            >
              +
            </button>
            <span className="text-sm text-slate-500">sessions / day</span>
          </div>
        )}
      </div>

      <div className="flex justify-end">
        <Button onClick={handleSave} disabled={isPending || isLoading}>
          {isPending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </div>
  )
}
