export function ISPLogoChip({
  isp,
  logoUrl,
  size = 32,
}: {
  isp: string
  logoUrl?: string
  size?: number
}) {
  return (
    <div className="isp-logo-chip" style={{ width: size, height: size }}>
      {logoUrl ? (
        <img src={logoUrl} alt="" className="isp-logo-image" />
      ) : (
        <span className="isp-logo-fallback">{isp.charAt(0).toUpperCase()}</span>
      )}
    </div>
  )
}
