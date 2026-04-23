import { useStore } from '../store'
import { hasPerm, type Permission } from '../lib/perm'

interface Props {
  perm: Permission
  fallback?: React.ReactNode
  children: React.ReactNode
}

export default function RequirePerm({ perm, fallback = null, children }: Props) {
  const me = useStore((s) => s.me)
  if (!me || !hasPerm(me.groups, perm)) return <>{fallback}</>
  return <>{children}</>
}
