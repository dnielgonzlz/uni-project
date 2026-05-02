import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { CalendarDays, Trash2, Users } from 'lucide-react'
import AddClientDialog from '@/components/clients/AddClientDialog'
import ClientDetailModal from '@/components/clients/ClientDetailModal'
import { useCoachClients, useDeleteCoachClient } from '@/hooks/useClients'
import { Badge } from '@/components/ui/badge'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

export const Route = createFileRoute('/dashboard/clients')({
  component: ClientsPage,
  head: () => ({ meta: [{ title: 'Clients — PT Scheduler' }] }),
})

function ClientsPage() {
  const [open, setOpen] = useState(false)
  const [pendingDeleteId, setPendingDeleteId] = useState<string | null>(null)
  const [selectedClientId, setSelectedClientId] = useState<string | null>(null)
  const [selectedClientName, setSelectedClientName] = useState('')
  const { data: clients, isLoading } = useCoachClients()
  const removeClient = useDeleteCoachClient()

  function openDetail(clientId: string, name: string) {
    setSelectedClientId(clientId)
    setSelectedClientName(name)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">Clients</h1>
          <p className="text-sm text-slate-500 mt-1">Your current clients and their session counts.</p>
        </div>
        <Button onClick={() => setOpen(true)}>Add client</Button>
      </div>

      {isLoading ? (
        <div className="space-y-3">
          {[1, 2, 3].map((i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
        </div>
      ) : !clients || clients.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center bg-white rounded-xl border border-slate-200">
          <Users className="h-10 w-10 text-slate-300 mb-3" />
          <p className="font-medium text-slate-600">No clients yet</p>
          <p className="text-sm text-slate-400 mt-1">Add your first client to start scheduling sessions.</p>
          <Button className="mt-5" onClick={() => setOpen(true)}>Add client</Button>
        </div>
      ) : (
        <div className="space-y-3">
          {clients.map((item) => {
            return (
              <div key={item.client.id} className="flex items-center justify-between bg-white rounded-xl border border-slate-200 px-5 py-4">
                <div>
                  <div className="flex items-center gap-2">
                    <p className="font-medium text-slate-900">{item.user.full_name}</p>
                    {item.user.is_verified ? (
                      <Badge variant="outline" className="text-emerald-600 border-emerald-200 bg-emerald-50 text-xs">Active</Badge>
                    ) : (
                      <Badge variant="outline" className="text-amber-600 border-amber-200 bg-amber-50 text-xs">Pending setup</Badge>
                    )}
                  </div>
                  <p className="text-sm text-slate-500">{item.user.email}</p>
                </div>
                <div className="flex items-center gap-3">
                  <div className="text-right">
                    <p className="text-sm font-medium text-slate-700">
                      {item.confirmed_session_count} confirmed
                    </p>
                    <p className="text-xs text-slate-400">{item.client.sessions_per_month} sessions/month quota</p>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon"
                    title="View availability & next session"
                    onClick={() => openDetail(item.client.id, item.user.full_name)}
                  >
                    <CalendarDays className="h-4 w-4 text-slate-500" />
                  </Button>
                  <AlertDialog>
                    <AlertDialogTrigger
                      render={(
                        <Button variant="ghost" size="icon" title="Remove client" />
                      )}
                    >
                      <Trash2 className="h-4 w-4 text-slate-500" />
                    </AlertDialogTrigger>
                    <AlertDialogContent size="sm">
                      <AlertDialogHeader>
                        <AlertDialogTitle>Remove client?</AlertDialogTitle>
                        <AlertDialogDescription>
                          {item.user.full_name} will be removed from your active client list and future active sessions will be hidden.
                        </AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel disabled={removeClient.isPending}>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                          variant="destructive"
                          disabled={removeClient.isPending}
                          onClick={async () => {
                            setPendingDeleteId(item.client.id)
                            try {
                              await removeClient.mutateAsync(item.client.id)
                            } finally {
                              setPendingDeleteId(null)
                            }
                          }}
                        >
                          {removeClient.isPending && pendingDeleteId === item.client.id ? 'Removing…' : 'Remove client'}
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                </div>
              </div>
            )
          })}
        </div>
      )}

      <AddClientDialog open={open} onClose={() => setOpen(false)} />

      <ClientDetailModal
        clientId={selectedClientId}
        clientName={selectedClientName}
        onClose={() => setSelectedClientId(null)}
      />
    </div>
  )
}
