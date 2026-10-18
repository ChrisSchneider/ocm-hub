import { ApiError } from '../api/client'

export function ErrorPanel({ error }: { error: unknown }) {
  const code = error instanceof ApiError ? error.code : 'ERROR'
  const message = error instanceof Error ? error.message : String(error)
  return (
    <div role="alert" className="rounded-md border border-red-300 bg-red-50 p-4 text-sm text-red-800">
      <span className="font-mono font-semibold">{code}</span> {message}
    </div>
  )
}
