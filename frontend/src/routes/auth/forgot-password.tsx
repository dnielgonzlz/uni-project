import { createFileRoute, Link } from '@tanstack/react-router'
import { useState } from 'react'
import api from '@/lib/api'
import AuthLayout from '@/components/layout/AuthLayout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export const Route = createFileRoute('/auth/forgot-password')({
  component: ForgotPasswordPage,
})

function ForgotPasswordPage() {
  const [email, setEmail] = useState('')
  const [submitted, setSubmitted] = useState(false)
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    try {
      await api.post('/auth/forgot-password', { email })
    } catch {
      // backend always returns 200 — show success regardless
    } finally {
      setLoading(false)
      setSubmitted(true)
    }
  }

  return (
    <AuthLayout>
      <h2 className="text-xl font-semibold text-slate-900 mb-2">Reset your password</h2>
      {submitted ? (
        <div className="space-y-4">
          <p className="text-sm text-slate-600">
            If an account exists for <strong>{email}</strong>, a reset link has been sent.
            Check your inbox and spam folder.
          </p>
          <Link to="/auth/login" className="text-sm text-primary hover:underline">
            Back to sign in
          </Link>
        </div>
      ) : (
        <>
          <p className="text-sm text-slate-500 mb-6">
            Enter your email and we'll send you a reset link.
          </p>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                autoComplete="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? 'Sending…' : 'Send reset link'}
            </Button>
          </form>
          <p className="text-sm text-slate-500 text-center mt-6">
            <Link to="/auth/login" className="text-primary hover:underline">
              Back to sign in
            </Link>
          </p>
        </>
      )}
    </AuthLayout>
  )
}
