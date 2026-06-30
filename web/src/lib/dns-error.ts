export type DnsErrorType = 'nxdomain' | 'timeout' | 'servfail' | 'other' | 'none'

// classifyDNSError maps raw Go error strings (stored in ScanResult.error) to a
// human-readable category. Compliant rows (DNS failed) have non-empty errors;
// violation rows (DNS resolved) have empty errors → 'none'.
export function classifyDNSError(error: string): DnsErrorType {
  if (!error) return 'none'
  const e = error.toLowerCase()
  if (e.includes('no such host') || e.includes('nxdomain')) return 'nxdomain'
  if (e.includes('timeout') || e.includes('i/o timeout') || e.includes('deadline exceeded')) return 'timeout'
  if (e.includes('server misbehaving') || e.includes('connection refused') || e.includes('servfail')) return 'servfail'
  return 'other'
}

export function dnsErrorLabel(type: DnsErrorType): string {
  switch (type) {
    case 'nxdomain': return 'NXDOMAIN'
    case 'timeout':  return 'Timeout'
    case 'servfail': return 'Server Error'
    case 'other':    return 'Error'
    case 'none':     return ''
  }
}
