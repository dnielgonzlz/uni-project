import { createFileRoute, redirect, Outlet } from '@tanstack/react-router'
import { useAuthStore } from '@/store/auth'
import CoachLayout from '@/components/layout/CoachLayout'
import { ensureAuthState } from '@/hooks/useAuth'

export const Route = createFileRoute('/dashboard')({
  beforeLoad: async () => {
    const { accessToken, user, profileId } = useAuthStore.getState()

    if (!accessToken || !user || !profileId) {
      const restoredUser = await ensureAuthState()
      if (!restoredUser) {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        throw redirect({ to: '/auth/login' as any })
      }
    }

    const currentUser = useAuthStore.getState().user

    if (currentUser && currentUser.role !== 'coach') {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      throw redirect({ to: '/client' as any })
    }
  },
  component: () => (
    <CoachLayout>
      <Outlet />
    </CoachLayout>
  ),
})
