import { Link, useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Building2, KeyRound, LogOut } from 'lucide-react'
import { api, type User } from '@/lib/api'
import { Button } from '@/components/ui'

export default function Layout({ user, children }: { user: User; children: React.ReactNode }) {
  const qc = useQueryClient()
  const nav = useNavigate()
  const logout = async () => {
    await api.logout()
    qc.clear()
    nav('/login')
  }
  return (
    <div className="flex min-h-full flex-col">
      <header className="sticky top-0 z-40 border-b border-border bg-white/95 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-7xl items-center gap-4 px-4">
          <Link to="/" className="flex items-center gap-2 font-semibold">
            <span className="flex size-7 items-center justify-center rounded-md bg-primary text-white"><Building2 className="size-4" /></span>
            SiteDocs
          </Link>
          <nav className="ml-4 flex items-center gap-1 text-sm">
            <Link to="/" className="rounded-md px-3 py-1.5 hover:bg-muted">My projects</Link>
            <Link to="/settings/tokens" className="flex items-center gap-1.5 rounded-md px-3 py-1.5 hover:bg-muted">
              <KeyRound className="size-4" />Access Tokens & MCP
            </Link>
          </nav>
          <div className="ml-auto flex items-center gap-3 text-sm">
            <span className="hidden text-muted-foreground sm:inline" title={user.email}>{user.name}</span>
            <Button variant="ghost" size="sm" onClick={logout}><LogOut />Sign out</Button>
          </div>
        </div>
      </header>
      <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-6">{children}</main>
    </div>
  )
}
