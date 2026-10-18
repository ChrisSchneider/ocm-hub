import { Container, File, Folder, GitBranch, Package, ShipWheel, SquareTerminal, type LucideIcon } from 'lucide-react'

const byType: Record<string, LucideIcon> = {
  ociImage: Container,
  ociArtifact: Container,
  helmChart: ShipWheel,
  executable: SquareTerminal,
  directoryTree: Folder,
  fileSystem: Folder,
}

export function TypeIcon({ type, source }: { type?: string; source?: boolean }) {
  const Icon = source ? GitBranch : ((type && byType[type]) || File)
  return <Icon aria-hidden size={16} className="shrink-0 text-slate-500" />
}

export function ComponentIcon() {
  return <Package aria-hidden size={16} className="shrink-0 text-slate-500" />
}
