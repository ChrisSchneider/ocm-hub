import { useEffect } from 'react'
import { useLocation } from '@tanstack/react-router'

// ScrollToHash: linked from other views and tabs; scroll to the anchor once the
// attestation table has settled (it shifts what follows).
export function ScrollToHash({ settled }: { settled: boolean }) {
  const hash = useLocation({ select: (l) => l.hash })
  useEffect(() => {
    if (!settled || !hash) return
    let id = hash
    try {
      id = decodeURIComponent(hash)
    } catch {
      // A malformed hash (e.g. "%E0") is used as is.
    }
    document.getElementById(id)?.scrollIntoView?.()
  }, [settled, hash])
  return null
}
