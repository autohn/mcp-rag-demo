import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { UserMinus, UserPlus } from 'lucide-react'
import { api, type Member, type Project, type Role } from '@/lib/api'
import { roleLabel } from '@/lib/utils'
import { Badge, Button, Card, ErrorText, Input, Select, Spinner } from '@/components/ui'
import { ConfirmDialog } from '@/components/Dialogs'
import { useMe } from '@/App'

export default function MembersTab({ project }: { project: Project }) {
  const pid = project.id
  const isAdmin = project.role === 'admin'
  const me = useMe().data
  const qc = useQueryClient()
  const nav = useNavigate()
  const members = useQuery({ queryKey: ['members', pid], queryFn: () => api.members(pid) })

  const [email, setEmail] = useState('')
  const [role, setRole] = useState<Role>('participant')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [removing, setRemoving] = useState<Member | null>(null)

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['members', pid] })
    qc.invalidateQueries({ queryKey: ['project', pid] })
    qc.invalidateQueries({ queryKey: ['projects'] })
  }

  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.addMember(pid, email, role)
      setEmail('')
      refresh()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  const changeRole = async (m: Member, r: Role) => {
    setError(null)
    try {
      await api.setRole(pid, m.user_id, r)
      refresh()
    } catch (err) {
      setError(err)
    }
  }

  return (
    <div className="max-w-3xl space-y-4">
      {isAdmin && (
        <Card className="p-4">
          <form onSubmit={add} className="flex flex-wrap items-end gap-2">
            <div className="min-w-56 flex-1">
              <label className="mb-1.5 block text-sm font-medium" htmlFor="m-email">Add member</label>
              <Input id="m-email" type="email" required placeholder="email of a registered user" value={email} onChange={(e) => setEmail(e.target.value)} />
            </div>
            <Select value={role} onChange={(e) => setRole(e.target.value as Role)} className="w-40" aria-label="Role">
              <option value="participant">Participant</option>
              <option value="admin">Admin</option>
            </Select>
            <Button type="submit" loading={busy}><UserPlus />Add</Button>
          </form>
        </Card>
      )}
      <ErrorText error={error} />
      <Card>
        {members.isLoading && <div className="p-4"><Spinner /></div>}
        <ul>
          {members.data?.map((m) => (
            <li key={m.user_id} className="flex items-center gap-3 border-b border-border px-4 py-3 last:border-0">
              <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-sm font-medium">
                {(m.name || m.email)[0]?.toUpperCase()}
              </span>
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium">{m.name}{m.user_id === me?.id && <span className="text-muted-foreground"> (you)</span>}</div>
                <div className="truncate text-xs text-muted-foreground">{m.email}</div>
              </div>
              {isAdmin ? (
                <>
                  <Select value={m.role} onChange={(e) => changeRole(m, e.target.value as Role)} className="h-8 w-36" aria-label="Role">
                    <option value="participant">Participant</option>
                    <option value="admin">Admin</option>
                  </Select>
                  <Button variant="ghost" size="icon" onClick={() => setRemoving(m)} aria-label="Remove from project" title="Remove from project">
                    <UserMinus />
                  </Button>
                </>
              ) : (
                <Badge variant={m.role === 'admin' ? 'blue' : 'default'}>{roleLabel(m.role)}</Badge>
              )}
            </li>
          ))}
        </ul>
      </Card>
      {!isAdmin && <p className="text-xs text-muted-foreground">Only a project Admin can manage members.</p>}
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(v) => !v && setRemoving(null)}
        title="Remove member?"
        description={removing && <>{removing.name} ({removing.email}) will lose access to the project.</>}
        confirmLabel="Remove"
        onConfirm={async () => {
          if (!removing) return
          await api.removeMember(pid, removing.user_id)
          if (removing.user_id === me?.id) {
            qc.invalidateQueries({ queryKey: ['projects'] })
            nav('/')
          } else refresh()
        }}
      />
    </div>
  )
}
