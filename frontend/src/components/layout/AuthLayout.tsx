import { ProductLogo } from '@/components/branding/product-logo'

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-slate-50 flex items-center justify-center p-4">
      <div className="w-full max-w-md">
        <div className="text-center mb-8">
          <ProductLogo
            variant="md"
            className="h-16 w-16 mx-auto mb-4 rounded-2xl shadow-md ring-1 ring-slate-200/80"
          />
          <h1 className="text-2xl font-bold text-slate-900">PT Scheduler</h1>
          <p className="text-sm text-slate-500 mt-1">Intelligent scheduling for personal trainers</p>
        </div>
        <div className="bg-white rounded-xl shadow-sm border border-slate-200 p-8">
          {children}
        </div>
      </div>
    </div>
  )
}
