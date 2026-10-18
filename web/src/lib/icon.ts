// componentIcon: the component's own icon, else the avatar of the GitHub org
// hosting it (requested at 96px; the API stores small 32px avatars).
export function componentIcon(c: { icon?: string; ownerAvatarUrl?: string }): string | undefined {
  if (c.icon) return c.icon
  if (!c.ownerAvatarUrl) return undefined
  try {
    const u = new URL(c.ownerAvatarUrl)
    if (u.searchParams.has('s')) u.searchParams.set('s', '96')
    return u.toString()
  } catch {
    return undefined
  }
}
