import { useRef } from 'react'
import { cn } from '@/lib/utils'

const OCTET_MAX = 3
const PORT_MAX = 5

// "1.2.3.4:853" -> ['1','2','3','4','853']; unparseable input (e.g. a DoH URL) -> five blanks.
function parseAddress(address: string): string[] {
  const m = /^(\d{0,3})\.(\d{0,3})\.(\d{0,3})\.(\d{0,3})(?::(\d{0,5}))?$/.exec(address.trim())
  if (!m) return ['', '', '', '', '']
  return [m[1], m[2], m[3], m[4], m[5] ?? '']
}

function joinAddress(segments: string[]): string {
  const [o1, o2, o3, o4, port] = segments
  const host = [o1, o2, o3, o4].join('.')
  return port ? `${host}:${port}` : host
}

const segmentClass = 'py-1.5 text-sm text-center font-[inherit] text-foreground bg-background border border-stone-border rounded-lg outline-none [font-variant-numeric:tabular-nums] transition-colors duration-150 ease-snappy focus:border-stone-muted disabled:opacity-50 disabled:cursor-not-allowed'

/** Segmented IPv4 + port entry: 4 octet boxes + a port box, auto-advancing as each fills. */
export function IPPortInput({
  value,
  onChange,
  onBlur,
  disabled,
  portPlaceholder,
}: {
  value: string
  onChange: (address: string) => void
  onBlur?: () => void
  disabled?: boolean
  portPlaceholder?: string
}) {
  const segments = parseAddress(value)
  const refs = useRef<Array<HTMLInputElement | null>>([])

  const setSegment = (i: number, raw: string) => {
    const max = i < 4 ? OCTET_MAX : PORT_MAX
    let digits = raw.replace(/\D/g, '').slice(0, max)
    if (digits !== '') {
      const capped = i < 4 ? 255 : 65535
      if (Number(digits) > capped) digits = String(capped)
    }
    const next = [...segments]
    next[i] = digits
    onChange(joinAddress(next))
    if (digits.length === max && i < 4) refs.current[i + 1]?.focus()
  }

  const handleKeyDown = (i: number) => (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === '.' && i < 3) {
      e.preventDefault()
      refs.current[i + 1]?.focus()
    } else if (e.key === ':' && i === 3) {
      e.preventDefault()
      refs.current[4]?.focus()
    } else if (e.key === 'Backspace' && segments[i] === '' && i > 0) {
      e.preventDefault()
      refs.current[i - 1]?.focus()
    }
  }

  // Pasting a full "1.2.3.4:853" string into any box fills the whole group.
  const handlePaste = (e: React.ClipboardEvent<HTMLInputElement>) => {
    const text = e.clipboardData.getData('text').trim()
    if (!/[.:]/.test(text)) return
    const parsed = parseAddress(text)
    if (!parsed.some(Boolean)) return
    e.preventDefault()
    onChange(joinAddress(parsed))
  }

  // Fires once when focus actually leaves the group, not when hopping between its own boxes.
  const handleGroupBlur = (e: React.FocusEvent<HTMLDivElement>) => {
    if (!e.currentTarget.contains(e.relatedTarget as Node | null)) onBlur?.()
  }

  return (
    <div className="flex items-center gap-1" onBlur={handleGroupBlur}>
      {[0, 1, 2, 3].map(i => (
        <div key={i} className="flex items-center gap-1">
          <input
            ref={el => { refs.current[i] = el }}
            className={cn(segmentClass, 'w-10')}
            inputMode="numeric"
            value={segments[i]}
            onChange={e => setSegment(i, e.target.value)}
            onKeyDown={handleKeyDown(i)}
            onPaste={handlePaste}
            onFocus={e => e.target.select()}
            disabled={disabled}
            aria-label={`IP address octet ${i + 1}`}
            maxLength={OCTET_MAX}
          />
          {i < 3 && <span className="text-stone-muted">.</span>}
        </div>
      ))}
      <span className="text-stone-muted">:</span>
      <input
        ref={el => { refs.current[4] = el }}
        className={cn(segmentClass, 'w-14')}
        inputMode="numeric"
        value={segments[4]}
        onChange={e => setSegment(4, e.target.value)}
        onKeyDown={handleKeyDown(4)}
        onPaste={handlePaste}
        onFocus={e => e.target.select()}
        disabled={disabled}
        placeholder={portPlaceholder}
        aria-label="Port"
        maxLength={PORT_MAX}
      />
    </div>
  )
}
