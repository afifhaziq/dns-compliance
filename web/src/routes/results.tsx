import { createFileRoute, Outlet } from '@tanstack/react-router'

export const Route = createFileRoute('/results')({ component: () => <Outlet /> })
