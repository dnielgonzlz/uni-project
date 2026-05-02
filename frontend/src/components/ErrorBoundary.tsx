import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { ProductLogo } from '@/components/branding/product-logo'

interface Props {
  children: ReactNode
}

interface State {
  hasError: boolean
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false }

  static getDerivedStateFromError(): State {
    return { hasError: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('ErrorBoundary caught:', error, info)
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex flex-col items-center justify-center min-h-[400px] text-center p-8">
          <ProductLogo variant="sm" className="h-10 w-10 mb-4 rounded-lg shadow-sm ring-1 ring-slate-200/80" />
          <p className="text-lg font-semibold text-slate-900 mb-2">Something went wrong</p>
          <p className="text-sm text-slate-500 mb-6">Try refreshing the page.</p>
          <Button onClick={() => window.location.reload()}>Refresh page</Button>
        </div>
      )
    }
    return this.props.children
  }
}
