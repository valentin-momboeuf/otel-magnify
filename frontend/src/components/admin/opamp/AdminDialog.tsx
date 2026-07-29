import { useLayoutEffect, useRef, type KeyboardEvent, type ReactNode, type RefObject } from 'react'

type AdminDialogProps = {
  ariaLabelledby: string
  children: ReactNode
  className?: string
  initialFocusRef?: RefObject<HTMLElement | null>
  onRequestClose?: () => void
  preventDismiss?: boolean
}

const focusableSelector = [
  'button:not([disabled])',
  'input:not([disabled])',
  'textarea:not([disabled])',
  'select:not([disabled])',
  'a[href]',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

export default function AdminDialog({
  ariaLabelledby,
  children,
  className = '',
  initialFocusRef,
  onRequestClose,
  preventDismiss = false,
}: AdminDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const restoreFocusRef = useRef<HTMLElement | null>(null)

  useLayoutEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return

    restoreFocusRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null
    dialog.showModal()
    initialFocusRef?.current?.focus()

    return () => {
      if (dialog.open) dialog.close()
      const restoreFocus = restoreFocusRef.current
      if (restoreFocus?.isConnected) restoreFocus.focus()
    }
  }, [initialFocusRef])

  const handleKeyDown = (event: KeyboardEvent<HTMLDialogElement>) => {
    if (event.key !== 'Tab') return
    const dialog = dialogRef.current
    if (!dialog) return

    const focusable = Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector))
    if (focusable.length === 0) {
      event.preventDefault()
      dialog.focus()
      return
    }

    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }

  return (
    <dialog
      ref={dialogRef}
      className={`opamp-token-dialog ${className}`.trim()}
      aria-labelledby={ariaLabelledby}
      onCancel={(event) => {
        event.preventDefault()
        if (!preventDismiss) onRequestClose?.()
      }}
      onClick={(event) => {
        const bounds = event.currentTarget.getBoundingClientRect()
        const clickedOutsidePanel =
          event.clientX < bounds.left ||
          event.clientX > bounds.right ||
          event.clientY < bounds.top ||
          event.clientY > bounds.bottom
        if (event.target === event.currentTarget && clickedOutsidePanel && !preventDismiss) {
          onRequestClose?.()
        }
      }}
      onKeyDown={handleKeyDown}
    >
      {children}
    </dialog>
  )
}
