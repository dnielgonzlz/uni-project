import { Loader2 } from 'lucide-react'

export default function SolverStatusBanner() {
  return (
    <div className="flex items-center gap-3 bg-primary text-primary-foreground rounded-xl px-5 py-4">
      <Loader2 className="h-5 w-5 animate-spin shrink-0" />
      <div>
        <p className="font-medium">Calculating your optimal schedule…</p>
        <p className="text-sm opacity-80">This may take up to 30 seconds. Please wait.</p>
      </div>
    </div>
  )
}
