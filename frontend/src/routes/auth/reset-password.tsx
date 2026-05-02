import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { useState } from 'react'
import { toast } from 'sonner'
import api from '@/lib/api'
import { ApiError } from '@/lib/api'
import AuthLayout from '@/components/layout/AuthLayout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export const Route = createFileRoute('/auth/reset-password')({
  validateSearch: (search: Record<string, unknown>) => ({
    token: (search.token as string) ?? '',
  }),
  component: ResetPasswordPage,
})

function ResetPasswordPage() {
  const navigate = useNavigate()
  const { token } = Route.useSearch()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [loading, setLoading] = useState(false)
  const [matchError, setMatchError] = useState('')

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (password !== confirm) {
      setMatchError('Passwords do not match.')
      return
    }
    setMatchError('')
    setLoading(true)
    try {
      await api.post('/auth/reset-password', { token, password })
      toast.success('Password updated. You can now sign in.')
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      navigate({ to: '/auth/login' as any })
    } catch (err) {
      if (err instanceof ApiError && (err.status === 422 || err.status === 400)) {
        toast.error('This reset link is invalid or has expired.')
      } else {
        toast.error('Something went wrong. Please try again.')
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <AuthLayout>
      <h2 className="text-xl font-semibold text-slate-900 mb-6">Set a new password</h2>
      {!token ? (
        <p className="text-sm text-red-600">
          Invalid reset link.{' '}
          <Link to="/auth/forgot-password" className="underline">
            Request a new one.
          </Link>
        </p>
      ) : (
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="password">New password</Label>
            <Input
              id="password"
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={8}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="confirm">Confirm password</Label>
            <Input
              id="confirm"
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              required
            />
            {matchError && <p className="text-xs text-red-600">{matchError}</p>}
          </div>
          <Button type="submit" className="w-full" disabled={loading}>
            {loading ? 'Updating…' : 'Update password'}
          </Button>
        </form>
      )}
    </AuthLayout>
  )
}
