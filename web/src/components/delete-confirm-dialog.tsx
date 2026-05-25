import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'

type Props = {
  open: boolean
  itemLabel: string
  onConfirm: () => Promise<void>
  onCancel: () => void
}

export function DeleteConfirmDialog({ open, itemLabel, onConfirm, onCancel }: Props) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleConfirm = async () => {
    setLoading(true)
    setError(null)
    try {
      await onConfirm()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed')
    } finally {
      setLoading(false)
    }
  }

  const handleCancel = () => {
    if (loading) return
    setError(null)
    onCancel()
  }

  return (
    <Dialog open={open} onOpenChange={open => { if (!open) handleCancel() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 400 }}>
        <DialogHeader>
          <DialogTitle>Delete "{itemLabel}"?</DialogTitle>
          <DialogDescription>
            This will remove it from all future scans.
          </DialogDescription>
        </DialogHeader>
        {error && <p className="form-error">{error}</p>}
        <DialogFooter>
          <button className="btn-ghost" onClick={handleCancel} disabled={loading}>
            Cancel
          </button>
          <button className="btn-danger" onClick={handleConfirm} disabled={loading}>
            {loading ? 'Deleting…' : 'Delete'}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
