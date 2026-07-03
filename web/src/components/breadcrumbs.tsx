import { Link } from '@tanstack/react-router'
import { ChevronRightIcon } from 'lucide-react'

type Crumb = {
  label: string
  to?: string
  params?: Record<string, string>
}

export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav className="breadcrumbs mt-8" aria-label="Breadcrumb">
      {items.map((item, i) => {
        const isLast = i === items.length - 1
        return (
          <span key={i} className="breadcrumb-item">
            {item.to && !isLast ? (
              <Link to={item.to} params={item.params} className="breadcrumb-link">
                {item.label}
              </Link>
            ) : (
              <span className="breadcrumb-current">{item.label}</span>
            )}
            {!isLast && <ChevronRightIcon className="breadcrumb-sep" />}
          </span>
        )
      })}
    </nav>
  )
}
