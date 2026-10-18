// Display names of components: the title label, else the last name segment
// with - and _ as spaces and each word capitalised.
export function humanize(segment: string): string {
  return segment
    .split(/[-_]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}

export function splitName(name: string): { namespace: string; last: string } {
  const i = name.lastIndexOf('/')
  return i < 0 ? { namespace: '', last: name } : { namespace: name.slice(0, i), last: name.slice(i + 1) }
}

// The API falls back to the name when there is no title label.
export function displayTitle(title: string | undefined, name: string): string {
  return title && title !== name ? title : humanize(splitName(name).last)
}
