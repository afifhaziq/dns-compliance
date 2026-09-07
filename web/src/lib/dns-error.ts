// DnsErrorType mirrors the categories internal/dns.Classify computes in Go
// and stores as ScanResult.error_class — see internal/dns/classify.go.
export type DnsErrorType = 'nxdomain' | 'timeout' | 'servfail' | 'refused' | 'empty' | 'invalid_url' | 'other' | 'none'

const KNOWN: DnsErrorType[] = ['nxdomain', 'timeout', 'servfail', 'refused', 'empty', 'invalid_url', 'other']

// classifyDNSError maps ScanResult.error_class (computed server-side, see
// internal/dns.Classify) to a typed category. Compliant rows (DNS failed)
// have a non-empty error_class; violation rows (DNS resolved) have '' →
// 'none'. Falls back to 'other' for any value this frontend doesn't
// recognize yet, rather than silently rendering nothing.
export function classifyDNSError(errorClass: string): DnsErrorType {
  if (!errorClass) return 'none'
  return (KNOWN as string[]).includes(errorClass) ? (errorClass as DnsErrorType) : 'other'
}

export function dnsErrorLabel(type: DnsErrorType): string {
  switch (type) {
    case 'nxdomain':    return 'NXDOMAIN'
    case 'timeout':     return 'Timeout'
    case 'servfail':    return 'Server Error'
    case 'refused':     return 'Refused'
    case 'empty':       return 'Empty Response'
    case 'invalid_url': return 'Invalid URL'
    case 'other':       return 'Error'
    case 'none':        return ''
  }
}
