import { Link, useLocation } from '@tanstack/react-router'
import {
  LayoutDashboard, Calendar, Clock, Users, CreditCard, CalendarDays, Menu, LogOut, Bot,
} from 'lucide-react'
import { useState } from 'react'
import { Sheet, SheetContent, SheetTrigger } from '@/components/ui/sheet'
import { Button } from '@/components/ui/button'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Separator } from '@/components/ui/separator'
import { useAuth } from '@/hooks/useAuth'
import { ProductLogo } from '@/components/branding/product-logo'

const NAV: Array<{ to: string; label: string; icon: React.ElementType; exact?: boolean }> = [
  { to: '/dashboard', label: 'Overview', icon: LayoutDashboard, exact: true },
  { to: '/dashboard/schedule', label: 'Schedule', icon: Calendar },
  { to: '/dashboard/availability', label: 'Availability', icon: Clock },
  { to: '/dashboard/clients', label: 'Clients', icon: Users },
  { to: '/dashboard/billing', label: 'Billing', icon: CreditCard },
  { to: '/dashboard/calendar', label: 'Calendar', icon: CalendarDays },
  { to: '/dashboard/agent-settings', label: 'AI Booking Agent', icon: Bot },
]

function NavLinks({ onNav }: { onNav?: () => void }) {
  const { pathname } = useLocation()

  return (
    <nav className="flex-1 px-3 space-y-0.5">
      {NAV.map(({ to, label, icon: Icon, exact }) => {
        const active = exact ? pathname === to : pathname.startsWith(to)
        return (
          <Link
            key={to}
            to={to as never}
            onClick={onNav}
            className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors ${
              active
                ? 'bg-primary/10 text-primary'
                : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900'
            }`}
          >
            <Icon className="h-4 w-4 shrink-0" />
            {label}
          </Link>
        )
      })}
    </nav>
  )
}

function SidebarContent({ onNav }: { onNav?: () => void }) {
  const { user, logout } = useAuth()
  const initials = user?.full_name
    .split(' ')
    .map((n) => n[0])
    .join('')
    .toUpperCase()
    .slice(0, 2) ?? 'PT'

  return (
    <div className="flex flex-col h-full">
      <div className="px-6 py-5">
        <Link
          to="/dashboard"
          className="flex items-center gap-2.5 rounded-lg outline-offset-2 focus-visible:outline-2 focus-visible:outline-primary/40"
        >
          <ProductLogo variant="sm" className="h-8 w-8 rounded-lg shadow-sm ring-1 ring-slate-200/80" />
          <span className="text-lg font-bold text-slate-900">PT Scheduler</span>
        </Link>
      </div>
      <Separator />
      <div className="flex-1 overflow-y-auto py-4">
        <NavLinks onNav={onNav} />
      </div>
      <Separator />
      <div className="p-4 flex items-center justify-between">
        <div className="flex items-center gap-2 min-w-0">
          <Avatar className="h-8 w-8 shrink-0">
            <AvatarFallback className="text-xs">{initials}</AvatarFallback>
          </Avatar>
          <div className="min-w-0">
            <p className="text-sm font-medium text-slate-900 truncate">{user?.full_name}</p>
            <p className="text-xs text-slate-500 truncate">{user?.email}</p>
          </div>
        </div>
        <Button variant="ghost" size="icon" onClick={logout} title="Sign out" className="shrink-0">
          <LogOut className="h-4 w-4" />
        </Button>
      </div>
    </div>
  )
}

export default function CoachLayout({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false)

  return (
    <div className="flex h-screen bg-slate-50">
      {/* Desktop sidebar */}
      <aside className="hidden md:flex w-60 shrink-0 flex-col bg-white border-r border-slate-200">
        <SidebarContent />
      </aside>

      {/* Mobile header + sheet */}
      <div className="flex flex-col flex-1 min-w-0">
        <header className="md:hidden flex items-center justify-between px-4 py-3 bg-white border-b border-slate-200">
          <Link
            to="/dashboard"
            className="flex items-center gap-2 rounded-lg outline-offset-2 focus-visible:outline-2 focus-visible:outline-primary/40 min-w-0"
          >
            <ProductLogo variant="sm" className="h-8 w-8 shrink-0 rounded-lg shadow-sm ring-1 ring-slate-200/80" />
            <span className="text-lg font-bold text-slate-900 truncate">PT Scheduler</span>
          </Link>
          <Sheet open={open} onOpenChange={setOpen}>
            <SheetTrigger render={<button className="p-2 rounded-md hover:bg-slate-100" />}>
              <Menu className="h-5 w-5" />
            </SheetTrigger>
            <SheetContent side="left" className="w-60 p-0" showCloseButton={false}>
              <SidebarContent onNav={() => setOpen(false)} />
            </SheetContent>
          </Sheet>
        </header>

        <main className="flex-1 overflow-y-auto p-6">{children}</main>
      </div>
    </div>
  )
}
