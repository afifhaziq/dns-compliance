import { useState, useEffect } from 'react'

export function ISPLogoChip({
  isp,
  logoUrl,
  size = 32,
}: {
  isp: string
  logoUrl?: string
  size?: number
}) {
  const [failed, setFailed] = useState(false)
  useEffect(() => { setFailed(false) }, [logoUrl])

  const showImage = logoUrl && !failed

  return (
    <div className="isp-logo-chip" style={{ width: size, height: size }}>
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
