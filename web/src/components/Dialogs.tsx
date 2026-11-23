import { useEffect, useState } from 'react'
import { Button, Dialog, DialogContent, ErrorText, Input, Label } from '@/components/ui'

/** A dialog asking for a single name; onSubmit may throw to show an error. */
export function NameDialog({ open, onOpenChange, title, label = 'Name', initial = '', submitLabel = 'Save', onSubmit }: {
  open: boolean
  onOpenChange: (v: boolean) => void
  title: string
  label?: string
  initial?: string
  submitLabel?: string
  onSubmit: (name: string) => Promise<unknown>
}) {
  const [value, setValue] = useState(initial)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  useEffect(() => {
    if (open) {
      setValue(initial)
      setError(null)
    }
  }, [open, initial])
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title={title}>
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault()
            setBusy(true)
            setError(null)
            try {
              await onSubmit(value)
              onOpenChange(false)
            } catch (err) {
              setError(err)
            } finally {
              setBusy(false)
            }
          }}
        >
          <div className="space-y-1.5">
            <Label htmlFor="name-input">{label}</Label>
            <Input id="name-input" autoFocus value={value} onChange={(e) => setValue(e.target.value)} />
          </div>
          <ErrorText error={error} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="submit" loading={busy} disabled={!value.trim()}>{submitLabel}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function ConfirmDialog({ open, onOpenChange, title, description, confirmLabel = 'Delete', onConfirm }: {
  open: boolean
  onOpenChange: (v: boolean) => void
  title: string
  description: React.ReactNode
  confirmLabel?: string
  onConfirm: () => Promise<unknown>
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  useEffect(() => { if (open) setError(null) }, [open])
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title={title} description={description}>
        <ErrorText error={error} />
        <div className="mt-2 flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="destructive"
            loading={busy}
            onClick={async () => {
              setBusy(true)
              try {
                await onConfirm()
                onOpenChange(false)
              } catch (err) {
                setError(err)
              } finally {
                setBusy(false)
              }
            }}
          >
            {confirmLabel}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
