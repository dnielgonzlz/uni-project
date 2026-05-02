import logo32 from '@/assets/pt_logo_32.svg'
import logo192 from '@/assets/pt_logo_192.svg'
import { cn } from '@/lib/utils'

const srcByVariant = {
  sm: logo32,
  md: logo192,
} as const

export type ProductLogoVariant = keyof typeof srcByVariant

/** Renders the PT Scheduler mark; `variant` picks the source asset for sharp scaling. */
export function ProductLogo({
  variant = 'sm',
  className,
  alt = 'PT Scheduler',
}: {
  variant?: ProductLogoVariant
  className?: string
  alt?: string
}) {
  return (
    <img
      src={srcByVariant[variant]}
      alt={alt}
      className={cn('shrink-0 select-none', className)}
      draggable={false}
    />
  )
}
