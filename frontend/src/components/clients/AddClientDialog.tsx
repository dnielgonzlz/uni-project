import { useState } from 'react'
import {
  Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useCreateCoachClient } from '@/hooks/useClients'
import { ApiError } from '@/lib/api'

interface Props {
  open: boolean
  onClose: () => void
}

const initialForm = {
  full_name: '',
  email: '',
  phone: '',
  timezone: 'Europe/London',
  sessions_per_month: '4',
}

export default function AddClientDialog({ open, onClose }: Props) {
  const createClient = useCreateCoachClient()
  const [form, setForm] = useState(initialForm)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  function set(key: keyof typeof form) {
    return (e: React.ChangeEvent<HTMLInputElement>) =>
      setForm((current) => ({ ...current, [key]: e.target.value }))
  }

  function handleClose() {
    setFieldErrors({})
    onClose()
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setFieldErrors({})

    try {
      await createClient.mutateAsync({
        email: form.email,
        full_name: form.full_name,
        phone: form.phone,
        timezone: form.timezone || undefined,
        sessions_per_month: Number(form.sessions_per_month),
      })
      setForm(initialForm)
      onClose()
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 409) {
          setFieldErrors({ email: 'An account with this email already exists.' })
          return
        }
        if (err.status === 422 && err.fields) {
          setFieldErrors(err.fields)
        }
      }
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && handleClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add client</DialogTitle>
        </DialogHeader>

        <form id="add-client-form" onSubmit={handleSubmit} className="space-y-4 py-2">
          <div className="space-y-1.5">
            <Label htmlFor="full_name">Full name</Label>
            <Input id="full_name" value={form.full_name} onChange={set('full_name')} required />
            {fieldErrors.full_name && <p className="text-xs text-red-600">{fieldErrors.full_name}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="email">Email</Label>
            <Input id="email" type="email" value={form.email} onChange={set('email')} required />
            {fieldErrors.email && <p className="text-xs text-red-600">{fieldErrors.email}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="phone">Phone <span className="text-slate-400 text-xs">(WhatsApp — required for availability bot)</span></Label>
            <Input id="phone" value={form.phone} onChange={set('phone')} placeholder="+447700900123" required />
            {fieldErrors.phone && <p className="text-xs text-red-600">{fieldErrors.phone}</p>}
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="timezone">Timezone</Label>
              <Input id="timezone" value={form.timezone} onChange={set('timezone')} required />
              {fieldErrors.timezone && <p className="text-xs text-red-600">{fieldErrors.timezone}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="sessions_per_month">Sessions per month</Label>
              <Input
                id="sessions_per_month"
                type="number"
                min="1"
                max="20"
                value={form.sessions_per_month}
                onChange={set('sessions_per_month')}
                required
              />
              {fieldErrors.sessions_per_month && (
                <p className="text-xs text-red-600">{fieldErrors.sessions_per_month}</p>
              )}
            </div>
          </div>
        </form>

        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={createClient.isPending}>
            Cancel
          </Button>
          <Button type="submit" form="add-client-form" disabled={createClient.isPending}>
            {createClient.isPending ? 'Adding…' : 'Add client'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
