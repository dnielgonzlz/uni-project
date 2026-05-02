import { createFileRoute } from '@tanstack/react-router'
import { useState, useEffect } from 'react'
import { Bot, CheckCircle2, XCircle, Clock, AlertCircle, PhoneOff } from 'lucide-react'
import { toast } from 'sonner'
import {
  useAgentSettings,
  useUpdateAgentSettings,
  useCheckAgentTemplate,
  useAgentClients,
  useUpdateAgentClient,
  useSendAgentCampaignNow,
} from '@/hooks/useAgentSettings'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import type { TemplateStatus, CheckTemplateResponse } from '@/lib/types'

export const Route = createFileRoute('/dashboard/agent-settings')({
  component: AgentSettingsPage,
  head: () => ({ meta: [{ title: 'AI Booking Agent — PT Scheduler' }] }),
})

function TemplateStatusBadge({ status }: { status: TemplateStatus }) {
  const config: Record<TemplateStatus, { label: string; className: string; icon: React.ElementType }> = {
    missing: { label: 'Not configured', className: 'bg-slate-100 text-slate-600', icon: AlertCircle },
    pending: { label: 'Pending approval', className: 'bg-amber-100 text-amber-700', icon: Clock },
    approved: { label: 'Approved', className: 'bg-emerald-100 text-emerald-700', icon: CheckCircle2 },
    rejected: { label: 'Rejected', className: 'bg-rose-100 text-rose-700', icon: XCircle },
  }
  const { label, className, icon: Icon } = config[status]
  return (
    <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${className}`}>
      <Icon className="h-3.5 w-3.5" />
      {label}
    </span>
  )
}

function Toggle({
  checked,
  onChange,
  disabled,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 ${
        checked ? 'bg-emerald-500' : 'bg-slate-200'
      }`}
    >
      <span
        className={`pointer-events-none inline-block h-5 w-5 rounded-full bg-white shadow-sm ring-0 transition-transform ${
          checked ? 'translate-x-5' : 'translate-x-0'
        }`}
      />
    </button>
  )
}

function AgentSettingsPage() {
  const { data: settings, isLoading: loadingSettings } = useAgentSettings()
  const { data: clients, isLoading: loadingClients } = useAgentClients()
  const updateSettings = useUpdateAgentSettings()
  const checkTemplate = useCheckAgentTemplate()
  const updateClient = useUpdateAgentClient()
  const sendNow = useSendAgentCampaignNow()

  const [isEnabled, setIsEnabled] = useState(false)
  const [templateSid, setTemplateSid] = useState('')
  const [checkResult, setCheckResult] = useState<CheckTemplateResponse | null>(null)

  useEffect(() => {
    if (settings) {
      setIsEnabled(settings.enabled)
      setTemplateSid(settings.template_sid ?? '')
    }
  }, [settings])

  const handleSave = () => {
    updateSettings.mutate({ enabled: isEnabled, template_sid: templateSid.trim() || null })
  }

  const handleCheckTemplate = async () => {
    const nextTemplateSid = templateSid.trim()
    if (!nextTemplateSid) {
      toast.error('Add a Template SID before checking approval.')
      return
    }

    try {
      // Persist the latest SID first so the backend checks the value on screen.
      await updateSettings.mutateAsync({ enabled: isEnabled, template_sid: nextTemplateSid })
      const result = await checkTemplate.mutateAsync()
      setCheckResult(result)
      toast.info(`Template status: ${result.template_status}`)
    } catch {
      // error surfaced via onError in the mutation
    }
  }

  const allowedCount = clients?.filter((c) => c.ai_booking_enabled).length ?? 0

  const promptLabel = settings
    ? `Every ${settings.prompt_day.charAt(0).toUpperCase() + settings.prompt_day.slice(1)} at ${settings.prompt_time.slice(0, 5)} (${settings.timezone})`
    : 'Every Friday at 18:00 (Europe/London)'

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900 flex items-center gap-2">
          <Bot className="h-6 w-6 text-slate-700" />
          AI Booking Agent
        </h1>
        <p className="text-slate-500 text-sm mt-1">
          Configure the AI agent that sends WhatsApp booking prompts to your clients.
        </p>
      </div>

      {/* Status overview cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <Card>
          <CardContent className="pt-5">
            <p className="text-xs font-medium text-slate-500 uppercase tracking-wide mb-2">Agent</p>
            {loadingSettings ? (
              <Skeleton className="h-6 w-24" />
            ) : (
              <span
                className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${
                  settings?.enabled
                    ? 'bg-emerald-100 text-emerald-700'
                    : 'bg-slate-100 text-slate-600'
                }`}
              >
                {settings?.enabled ? (
                  <CheckCircle2 className="h-3.5 w-3.5" />
                ) : (
                  <XCircle className="h-3.5 w-3.5" />
                )}
                {settings?.enabled ? 'Active' : 'Inactive'}
              </span>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardContent className="pt-5">
            <p className="text-xs font-medium text-slate-500 uppercase tracking-wide mb-2">
              WhatsApp Template
            </p>
            {loadingSettings ? (
              <Skeleton className="h-6 w-28" />
            ) : (
              <TemplateStatusBadge status={settings?.template_status ?? 'missing'} />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardContent className="pt-5">
            <p className="text-xs font-medium text-slate-500 uppercase tracking-wide mb-2">
              Allowed Clients
            </p>
            {loadingClients ? (
              <Skeleton className="h-6 w-12" />
            ) : (
              <span className="text-2xl font-bold text-slate-900">{allowedCount}</span>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Settings form */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Settings</CardTitle>
          <CardDescription>Control how the agent behaves.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          {/* Enabled toggle */}
          <div className="flex items-center justify-between">
            <div>
              <Label className="text-sm font-medium text-slate-900">Agent enabled</Label>
              <p className="text-xs text-slate-500 mt-0.5">
                Allow the agent to send WhatsApp prompts to allowed clients.
              </p>
            </div>
            {loadingSettings ? (
              <Skeleton className="h-6 w-11 rounded-full" />
            ) : (
              <Toggle checked={isEnabled} onChange={setIsEnabled} />
            )}
          </div>

          <Separator />

          {/* Template SID */}
          <div className="space-y-2">
            <Label htmlFor="template-sid" className="text-sm font-medium text-slate-900">
              WhatsApp Template SID
            </Label>
            <p className="text-xs text-slate-500">
              The approved message template SID (e.g.{' '}
              <code className="font-mono bg-slate-100 px-1 rounded">HX…</code>) from your
              messaging account.
            </p>
            <div className="flex gap-2">
              <Input
                id="template-sid"
                placeholder="HXxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
                value={templateSid}
                onChange={(e) => setTemplateSid(e.target.value)}
                className="font-mono text-sm"
              />
              <Button
                variant="outline"
                onClick={handleCheckTemplate}
                disabled={checkTemplate.isPending}
              >
                {checkTemplate.isPending ? 'Checking…' : 'Check status'}
              </Button>
            </div>
            {checkResult && (
              <div className="flex items-start gap-3 p-3 rounded-lg bg-slate-50 border border-slate-200">
                <TemplateStatusBadge status={checkResult.template_status} />
                {checkResult.rejection_reason && (
                  <p className="text-xs text-slate-600 mt-0.5">{checkResult.rejection_reason}</p>
                )}
              </div>
            )}
          </div>

          <Separator />

          {/* Fixed prompt schedule — read-only */}
          <div>
            <Label className="text-sm font-medium text-slate-900">Prompt schedule</Label>
            <p className="text-xs text-slate-500 mt-0.5">
              When the agent sends booking prompts. This schedule is fixed.
            </p>
            <div className="mt-2 flex items-center gap-2 px-3 py-2 rounded-lg bg-slate-50 border border-slate-200 text-sm text-slate-700">
              <Clock className="h-4 w-4 text-slate-400 shrink-0" />
              {loadingSettings ? (
                <Skeleton className="h-4 w-48" />
              ) : (
                <span className="font-medium">{promptLabel}</span>
              )}
            </div>
          </div>

          {/* Send now */}
          <div className="flex items-start justify-between gap-4 pt-1">
            <div>
              <Label className="text-sm font-medium text-slate-900">Send now</Label>
              <p className="text-xs text-slate-500 mt-0.5">
                Use this to test the approved WhatsApp template without waiting until Friday.
              </p>
            </div>
            <Button
              variant="outline"
              size="sm"
              className="shrink-0"
              onClick={() => sendNow.mutate()}
              disabled={
                sendNow.isPending ||
                loadingSettings ||
                !settings?.enabled ||
                settings?.template_status !== 'approved'
              }
            >
              {sendNow.isPending ? 'Sending…' : 'Send now'}
            </Button>
          </div>

          <Separator />

          {/* Coach confirmation — always on */}
          <div className="flex items-center justify-between">
            <div>
              <Label className="text-sm font-medium text-slate-900">
                Require coach confirmation
              </Label>
              <p className="text-xs text-slate-500 mt-0.5">
                All AI-proposed bookings must be confirmed by you before they are finalised.
                Always on.
              </p>
            </div>
            <Toggle checked={true} onChange={() => {}} disabled />
          </div>

          <Separator />

          <Button onClick={handleSave} disabled={updateSettings.isPending || loadingSettings}>
            {updateSettings.isPending ? 'Saving…' : 'Save settings'}
          </Button>
        </CardContent>
      </Card>

      {/* Allowed clients list */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Allowed clients</CardTitle>
          <CardDescription>
            Choose which clients the agent may contact. Clients without a phone number cannot
            receive WhatsApp messages.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loadingClients ? (
            <div className="space-y-3">
              {[1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-14 rounded-lg" />
              ))}
            </div>
          ) : !clients?.length ? (
            <p className="text-sm text-slate-500 py-6 text-center">No clients found.</p>
          ) : (
            <ul className="space-y-2">
              {clients.map((client) => (
                <li
                  key={client.client_id}
                  className="flex items-center justify-between px-3 py-3 rounded-lg border border-slate-200 bg-slate-50"
                >
                  <div className="min-w-0 mr-4">
                    <p className="text-sm font-medium text-slate-900 truncate">
                      {client.full_name}
                    </p>
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 mt-0.5">
                      {client.phone ? (
                        <span className="text-xs text-slate-500">{client.phone}</span>
                      ) : (
                        <span className="inline-flex items-center gap-1 text-xs text-amber-600 font-medium">
                          <PhoneOff className="h-3 w-3" />
                          No phone — WhatsApp unavailable
                        </span>
                      )}
                      <span className="text-xs text-slate-300">·</span>
                      <span className="text-xs text-slate-400 truncate">{client.email}</span>
                    </div>
                  </div>
                  <Toggle
                    checked={client.ai_booking_enabled}
                    onChange={(val) =>
                      updateClient.mutate({ clientId: client.client_id, enabled: val })
                    }
                    disabled={updateClient.isPending}
                  />
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
