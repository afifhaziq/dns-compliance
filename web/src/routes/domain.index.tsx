import { createFileRoute, redirect } from '@tanstack/react-router'

// The domain picker/browser was merged into /results' "All Time" tab.
export const Route = createFileRoute('/domain/')({
  beforeLoad: () => {
    throw redirect({ to: '/results', search: { tab: 'all-time' } })
  },
})
