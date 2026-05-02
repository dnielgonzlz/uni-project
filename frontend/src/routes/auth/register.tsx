import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { useState } from 'react'
import { toast } from 'sonner'
import { Dumbbell, User } from 'lucide-react'
import AuthLayout from '@/components/layout/AuthLayout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAuth } from '@/hooks/useAuth'
import { ApiError } from '@/lib/api'

export const Route = createFileRoute('/auth/register')({
  component: RegisterPage,
})

type Role = 'coach' | 'client'

function RegisterPage() {
  const navigate = useNavigate()
  const { register } = useAuth()
  const [step, setStep] = useState<1 | 2>(1)
  const [role, setRole] = useState<Role>('coach')
  const [loading, setLoading] = useState(false)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  const [form, setForm] = useState({
    email: '',
    password: '',
    full_name: '',
    business_name: '',
    coach_id: '',
    sessions_per_month: '4',
  })

  function set(key: keyof typeof form) {
    return (e: React.ChangeEvent<HTMLInputElement>) =>
      setForm((f) => ({ ...f, [key]: e.target.value }))
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setFieldErrors({})
    try {
      const body =
        role === 'coach'
          ? {
              email: form.email,
              password: form.password,
              full_name: form.full_name,
              role,
              business_name: form.business_name || undefined,
            }
          : {
              email: form.email,
              password: form.password,
              full_name: form.full_name,
              role,
              coach_id: form.coach_id,
              sessions_per_month: Number(form.sessions_per_month),
            }

      const user = await register(body)
      console.log('[register page] navigating to', user.role === 'coach' ? '/dashboard' : '/client')
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      await navigate({ to: user.role === 'coach' ? '/dashboard' : '/client' } as any)
      console.log('[register page] navigation complete')
    } catch (err) {
      console.error('[register page] caught error', err)
      if (err instanceof ApiError) {
        if (err.status === 409) {
          setFieldErrors({ email: 'An account with this email already exists.' })
        } else if (err.status === 422 && err.fields) {
          setFieldErrors(err.fields)
        } else {
          toast.error(err.message)
        }
      } else {
        toast.error('Something went wrong. Please try again.')
      }
    } finally {
      setLoading(false)
    }
  }

  if (step === 1) {
    return (
      <AuthLayout>
        <h2 className="text-xl font-semibold text-slate-900 mb-2">Create an account</h2>
        <p className="text-sm text-slate-500 mb-6">I am a…</p>
        <div className="grid grid-cols-2 gap-4">
          <button
            type="button"
            onClick={() => { setRole('coach'); setStep(2) }}
            className="flex flex-col items-center gap-3 p-6 rounded-xl border-2 border-primary bg-primary/5 hover:bg-primary/10 transition-colors"
          >
            <Dumbbell className="h-8 w-8 text-primary" />
            <span className="font-medium text-slate-900">Personal Trainer</span>
          </button>
          <button
            type="button"
            onClick={() => { setRole('client'); setStep(2) }}
            className="flex flex-col items-center gap-3 p-6 rounded-xl border-2 border-slate-200 hover:border-slate-300 hover:bg-slate-50 transition-colors"
          >
            <User className="h-8 w-8 text-slate-500" />
            <span className="font-medium text-slate-900">Client</span>
          </button>
        </div>
        <p className="text-sm text-slate-500 text-center mt-6">
          Already have an account?{' '}
          <Link to="/auth/login" className="text-primary hover:underline">
            Sign in
          </Link>
        </p>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout>
      <div className="flex items-center gap-2 mb-6">
        <button
          type="button"
          onClick={() => setStep(1)}
          className="text-sm text-slate-500 hover:text-slate-700"
        >
          ← Back
        </button>
        <h2 className="text-xl font-semibold text-slate-900">
          {role === 'coach' ? 'Personal Trainer' : 'Client'} account
        </h2>
      </div>
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="full_name">Full name</Label>
          <Input id="full_name" value={form.full_name} onChange={set('full_name')} required />
          {fieldErrors.full_name && (
            <p className="text-xs text-red-600">{fieldErrors.full_name}</p>
          )}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="email">Email</Label>
          <Input id="email" type="email" autoComplete="email" value={form.email} onChange={set('email')} required />
          {fieldErrors.email && <p className="text-xs text-red-600">{fieldErrors.email}</p>}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="password">Password</Label>
          <Input id="password" type="password" autoComplete="new-password" value={form.password} onChange={set('password')} required minLength={8} />
          {fieldErrors.password && <p className="text-xs text-red-600">{fieldErrors.password}</p>}
        </div>

        {role === 'coach' && (
          <div className="space-y-1.5">
            <Label htmlFor="business_name">Business name <span className="text-slate-400 text-xs">(optional)</span></Label>
            <Input id="business_name" value={form.business_name} onChange={set('business_name')} />
          </div>
        )}

        {role === 'client' && (
          <>
            <div className="space-y-1.5">
              <Label htmlFor="coach_id">Coach ID <span className="text-slate-400 text-xs">(ask your coach)</span></Label>
              <Input id="coach_id" value={form.coach_id} onChange={set('coach_id')} required />
              {fieldErrors.coach_id && <p className="text-xs text-red-600">{fieldErrors.coach_id}</p>}
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
            </div>
          </>
        )}

        <Button type="submit" className="w-full" disabled={loading}>
          {loading ? 'Creating account…' : 'Create account'}
        </Button>
      </form>
    </AuthLayout>
  )
}
