import { createFileRoute, redirect, Outlet } from '@tanstack/react-router'
import { useAuthStore } from '@/store/auth'
import ClientLayout from '@/components/layout/ClientLayout'
import { ensureAuthState } from '@/hooks/useAuth'

export const Route = createFileRoute('/client')({
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

    if (currentUser && currentUser.role !== 'client') {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      throw redirect({ to: '/dashboard' as any })
    }
  },
  component: () => (
    <ClientLayout>
      <Outlet />
    </ClientLayout>
  ),
})
