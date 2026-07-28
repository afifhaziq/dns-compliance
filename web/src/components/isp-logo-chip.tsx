import { useState, useEffect } from 'react'

// Samples the image's average non-transparent pixel color via canvas, so a
// logo with a transparent background gets a backdrop that matches its own
// color instead of a plain white square. Falls back to undefined (caller
// picks a default) if the source doesn't allow CORS pixel access.
function useAverageColor(src?: string): string | undefined {
  const [color, setColor] = useState<string | undefined>()

  useEffect(() => {
    setColor(undefined)
    if (!src) return
    let cancelled = false
    const img = new Image()
    img.crossOrigin = 'anonymous'
    img.referrerPolicy = 'no-referrer'
    img.onload = () => {
      if (cancelled) return
      try {
        const size = 16
        const canvas = document.createElement('canvas')
        canvas.width = size
        canvas.height = size
        const ctx = canvas.getContext('2d')
        if (!ctx) return
        ctx.drawImage(img, 0, 0, size, size)
        const { data } = ctx.getImageData(0, 0, size, size)
        let r = 0, g = 0, b = 0, weight = 0
        for (let i = 0; i < data.length; i += 4) {
          const alpha = data[i + 3]
          if (alpha === 0) continue
          r += data[i] * alpha
          g += data[i + 1] * alpha
          b += data[i + 2] * alpha
          weight += alpha
        }
        if (weight === 0) return
        setColor(`rgb(${Math.round(r / weight)}, ${Math.round(g / weight)}, ${Math.round(b / weight)})`)
      } catch {
        // canvas tainted by a non-CORS image host — leave color unset
      }
    }
    img.src = src
    return () => { cancelled = true }
  }, [src])

  return color
}

export function ISPLogoChip({
  isp,
  logoUrl,
  size = 32,
  background,
  matchLogoBackground = false,
}: {
  isp: string
  logoUrl?: string
  size?: number
  /** Overrides the chip's default backdrop — e.g. 'white' so a logo with a
   *  transparent background stays legible against the image itself. */
  background?: string
  /** Derive the backdrop from the logo's own average color instead of a
   *  fixed one; falls back to white if the image can't be sampled (CORS). */
  matchLogoBackground?: boolean
}) {
  const [failed, setFailed] = useState(false)
  useEffect(() => { setFailed(false) }, [logoUrl])

  const showImage = logoUrl && !failed
  const autoColor = useAverageColor(matchLogoBackground && showImage ? logoUrl : undefined)
  const resolvedBackground = background ?? (matchLogoBackground ? (autoColor ?? 'white') : undefined)

  return (
    <div className="isp-logo-chip" style={{ width: size, height: size, backgroundColor: resolvedBackground }}>
      {showImage ? (
        <img
          src={logoUrl}
          alt=""
          className="isp-logo-image"
          referrerPolicy="no-referrer"
          onError={() => setFailed(true)}
        />
      ) : (
        <span className="isp-logo-fallback" aria-hidden="true">{isp.charAt(0).toUpperCase()}</span>
      )}
    </div>
  )
}
