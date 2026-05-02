import { useState } from 'react'
import { Copy, RefreshCw, Info } from 'lucide-react'
import { toast } from 'sonner'
import { useCalendarURL, useRegenerateCalendarURL } from '@/hooks/useCalendar'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import {
  Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle,
} from '@/components/ui/dialog'

export default function CalendarURLCard() {
  const { data, isLoading } = useCalendarURL()
  const regenerate = useRegenerateCalendarURL()
  const [copied, setCopied] = useState(false)

  async function copy() {
    if (!data?.url) return
    await navigator.clipboard.writeText(data.url)
    setCopied(true)
    toast.success('URL copied to clipboard.')
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="bg-white rounded-xl border border-slate-200 p-6 space-y-5">
      <div>
        <div className="flex items-center gap-2">
          <h2 className="text-base font-semibold text-slate-900">Calendar subscription URL</h2>
          <Dialog>
            <DialogTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-slate-400 hover:text-slate-600"
                  title="How to add to your calendar"
                />
              }
            >
              <Info className="h-4 w-4" />
              <span className="sr-only">How to add to your calendar</span>
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>How to add to your calendar</DialogTitle>
              </DialogHeader>
              <div className="space-y-5 text-sm">
                <div>
                  <p className="font-medium text-slate-800 mb-2">Google Calendar</p>
                  <ol className="space-y-1.5 text-slate-600 list-decimal list-inside">
                    <li>Open Google Calendar on desktop</li>
                    <li>Click "+" next to "Other calendars" in the left sidebar</li>
                    <li>Select "From URL"</li>
                    <li>Paste your calendar URL and click "Add calendar"</li>
                    <li className="text-slate-500 italic">Updates may take up to 24 hours</li>
                  </ol>
                </div>
                <div>
                  <p className="font-medium text-slate-800 mb-2">Apple Calendar (macOS)</p>
                  <ol className="space-y-1.5 text-slate-600 list-decimal list-inside">
                    <li>Open Calendar → File → New Calendar Subscription…</li>
                    <li>Paste the URL and click Subscribe</li>
                    <li>Set Auto-refresh to "Every hour" and click OK</li>
                  </ol>
                </div>
                <div>
                  <p className="font-medium text-slate-800 mb-2">iPhone / iPad</p>
                  <ol className="space-y-1.5 text-slate-600 list-decimal list-inside">
                    <li>Go to Settings → Calendar → Accounts → Add Account → Other</li>
                    <li>Tap "Add Subscribed Calendar"</li>
                    <li>Paste the URL and tap Next</li>
                  </ol>
                </div>
              </div>
            </DialogContent>
          </Dialog>
        </div>
        <p className="text-sm text-slate-500 mt-1">
          Add this URL to Google Calendar, Apple Calendar, or any app that supports ICS feeds.
        </p>
      </div>

      {isLoading ? (
        <Skeleton className="h-10 w-full rounded-lg" />
      ) : (
        <div className="flex gap-2">
          <Input value={data?.url ?? ''} readOnly className="font-mono text-xs" />
          <Button variant="outline" size="icon" onClick={copy} title="Copy URL">
            <Copy className="h-4 w-4" />
            {copied && <span className="sr-only">Copied!</span>}
          </Button>
        </div>
      )}

      {data?.warning && (
        <div className="flex items-start gap-2 bg-blue-50 border border-blue-200 rounded-lg p-3">
          <Info className="h-4 w-4 text-blue-500 shrink-0 mt-0.5" />
          <p className="text-sm text-blue-800">{data.warning}</p>
        </div>
      )}

      <div className="pt-2 border-t border-slate-100">
        <p className="text-sm text-slate-500 mb-3">
          Regenerating the URL will invalidate any existing subscriptions.
        </p>
        <AlertDialog>
          <AlertDialogTrigger
            render={
              <Button variant="outline" size="sm" disabled={regenerate.isPending} />
            }
          >
            <RefreshCw className="h-3.5 w-3.5 mr-1.5" />
            {regenerate.isPending ? 'Regenerating…' : 'Regenerate URL'}
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Regenerate calendar URL?</AlertDialogTitle>
              <AlertDialogDescription>
                This will create a new URL and invalidate your current one.
                Any calendar apps using the old URL will stop receiving updates.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction onClick={() => regenerate.mutate()}>
                Regenerate
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </div>
  )
}
