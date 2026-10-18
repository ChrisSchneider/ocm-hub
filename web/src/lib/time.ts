const UNITS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ['year', 365 * 24 * 3600],
  ['month', 30 * 24 * 3600],
  ['day', 24 * 3600],
  ['hour', 3600],
  ['minute', 60],
]

// "2026-02-01"
export const formatDate = (iso: string) => new Date(iso).toISOString().slice(0, 10)

// "3 days ago", "last month"; undefined when the input is not a date.
export function relativeTime(iso: string, now = Date.now()): string | undefined {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return undefined
  const seconds = Math.round((t - now) / 1000)
  const fmt = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  for (const [unit, size] of UNITS) {
    if (Math.abs(seconds) >= size) return fmt.format(Math.round(seconds / size), unit)
  }
  return fmt.format(Math.round(seconds / 60), 'minute')
}
