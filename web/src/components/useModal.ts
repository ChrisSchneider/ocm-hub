import { useEffect, useRef, type RefObject } from 'react'

// Modal behaviour shared by dialogs: focus `initial` on open, close on Escape
// wherever focus is, and give focus back to the opener on close.
export function useModal(onClose: () => void, initial: RefObject<HTMLElement | null>) {
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null
    initial.current?.focus()
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onCloseRef.current()
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
      opener?.focus()
    }
  }, [initial])
}
