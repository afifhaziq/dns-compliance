import type { ReactNode } from 'react'
import { AuroraBars } from '@/components/unlumen-ui/primitives/effects/aurora-bars'

// Shared frame for the login and force-change-password screens — the two
// "nothing else is usable yet" moments. Aurora bars on the page, a frosted
// white panel on top. Reads theme tokens, so it follows light/dark.
export function AuthShell({
  title,
  subtitle,
  children,
  footer,
}: {
  title: ReactNode
  subtitle?: string
  children: ReactNode
  footer?: ReactNode
}) {
  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden px-4 py-10">
      <AuroraBars className="fixed inset-0 -z-10" gap={0} blur={3.142} />
      <div className="w-full max-w-[380px] rounded-[10px] bg-white/30 p-8 backdrop-blur-xl dark:bg-card/30">
        <h1 className="page-title">{title}</h1>
        {subtitle && <p className="page-subtitle mt-1.5">{subtitle}</p>}
        <div className="mt-6">{children}</div>
        {footer && <p className="mt-4 text-[13px] text-muted-foreground">{footer}</p>}
      </div>
    </main>
  )
}
