import { LazyMotion, domAnimation, m } from 'motion/react'

interface ShimmerButtonProps {
  text?: string
  disabled?: boolean
  scanning?: boolean
  duration?: number
  onClick?: () => void
  ariaLabel?: string
}

export function ShimmerButton({
  text = 'Run Scan',
  disabled = false,
  scanning = false,
  duration = 1.2,
  onClick,
  ariaLabel,
}: ShimmerButtonProps) {
  return (
    <LazyMotion features={domAnimation}>
      <button
        onClick={onClick}
        disabled={disabled}
        aria-label={ariaLabel}
        className="shimmer-btn"
      >
        {scanning ? (
          <span className="shimmer-btn-scanning">
            <span className="spinner shimmer-spinner" aria-hidden="true" />
            Scanning…
          </span>
        ) : (
          <m.span
            className="shimmer-btn-text"
            animate={{ backgroundPosition: ['0% 0%', '-200% 0%'] }}
            transition={{ repeat: Infinity, duration, ease: 'linear' }}
          >
            {text}
          </m.span>
        )}
      </button>
    </LazyMotion>
  )
}
