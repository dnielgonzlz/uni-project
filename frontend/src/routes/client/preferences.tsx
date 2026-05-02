import { createFileRoute } from '@tanstack/react-router'
import { useState, useEffect } from 'react'
import { useAuthStore } from '@/store/auth'
import { useClientPreferences, useSetClientPreferences } from '@/hooks/useAvailability'
import AvailabilityGrid from '@/components/availability/AvailabilityGrid'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { TimeSlot } from '@/lib/types'

export const Route = createFileRoute('/client/preferences')({
  component: PreferencesPage,
  head: () => ({ meta: [{ title: 'Preferences — PT Scheduler' }] }),
})

function PreferencesPage() {
  const profileId = useAuthStore((s) => s.profileId) ?? ''
  const { data, isLoading } = useClientPreferences(profileId)
  const save = useSetClientPreferences(profileId)
  const [slots, setSlots] = useState<TimeSlot[]>([])

  useEffect(() => {
    if (data) setSlots(data)
  }, [data])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">Session preferences</h1>
        <p className="text-sm text-slate-500 mt-1">
          Tell your coach when you prefer to train. The scheduler will try to book sessions within these times.
        </p>
      </div>

      <div className="bg-white rounded-xl border border-slate-200 p-6">
        {isLoading ? (
          <Skeleton className="h-64 w-full rounded-lg" />
        ) : (
          <AvailabilityGrid value={slots} onChange={setSlots} />
        )}
      </div>

      <div className="flex justify-end">
        <Button onClick={() => save.mutate(slots)} disabled={save.isPending || isLoading}>
          {save.isPending ? 'Saving…' : 'Save preferences'}
        </Button>
      </div>
    </div>
  )
}
