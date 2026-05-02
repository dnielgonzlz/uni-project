import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/store/auth'
import { ensureAuthState } from '@/hooks/useAuth'

export const Route = createFileRoute('/')({
  beforeLoad: async () => {
    let { user } = useAuthStore.getState()
    if (!user) {
      user = await ensureAuthState()
    }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    if (!user) throw redirect({ to: '/auth/login' as any })
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    if (user.role === 'coach') throw redirect({ to: '/dashboard' as any })
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    throw redirect({ to: '/client' as any })
  },
  component: () => null,
})
