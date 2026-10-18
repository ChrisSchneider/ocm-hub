// "1.28–1.29, 1.31": consecutive minors of one major become a range.
export function collapseMinors(versions: string[]): string {
  const parsed = versions.map((v) => v.split('.').map(Number) as [number, number])
  const parts: string[] = []
  let start = 0
  for (let i = 1; i <= parsed.length; i++) {
    const prev = parsed[i - 1]
    const cur = parsed[i]
    if (cur && cur[0] === prev[0] && cur[1] === prev[1] + 1) continue
    parts.push(start === i - 1 ? versions[start] : `${versions[start]}–${versions[i - 1]}`)
    start = i
  }
  return parts.join(', ')
}
