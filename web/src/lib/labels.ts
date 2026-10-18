export const VEX_LABEL = 'catalog.chrisschneider.dev/vex'

export type LabelValue = { kind: 'inline'; text: string } | { kind: 'json'; value: unknown }

// labelValue: scalars and flat arrays of scalars read inline, anything else as JSON.
export function labelValue(v: unknown): LabelValue {
  if (typeof v === 'string' || typeof v === 'boolean' || (typeof v === 'number' && Number.isFinite(v))) return { kind: 'inline', text: String(v) }
  if (Array.isArray(v) && v.every((x) => typeof x === 'string' || (typeof x === 'number' && Number.isFinite(x)))) return { kind: 'inline', text: v.join(', ') }
  return { kind: 'json', value: v }
}
