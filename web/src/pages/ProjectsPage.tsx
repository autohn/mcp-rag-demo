import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { FolderKanban, Plus } from 'lucide-react'
import { api } from '@/lib/api'
import { formatDate, roleLabel } from '@/lib/utils'
import { Badge, Button, Card, ErrorText, Spinner } from '@/components/ui'
import { NameDialog } from '@/components/Dialogs'

export default function ProjectsPage() {
  const projects = useQuery({ queryKey: ['projects'], queryFn: api.projects })
  const [creating, setCreating] = useState(false)
  const qc = useQueryClient()
  const nav = useNavigate()

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold">My projects</h1>
        <Button onClick={() => setCreating(true)}><Plus />New project</Button>
      </div>
      {projects.isLoading && <Spinner />}
      <ErrorText error={projects.error} />
      {projects.data?.length === 0 && (
        <Card className="p-10 text-center text-sm text-muted-foreground">
          You have no projects yet. Create one, or ask an Admin to add you by email.
        </Card>
      )}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {projects.data?.map((p) => (
          <Link key={p.id} to={`/projects/${p.id}/files`}>
            <Card className="flex h-full flex-col gap-3 p-4 transition hover:border-primary/40 hover:shadow">
              <div className="flex items-start gap-3">
                <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-accent text-primary"><FolderKanban className="size-5" /></span>
                <div className="min-w-0">
                  <div className="truncate font-medium">{p.name}</div>
                  <div className="text-xs text-muted-foreground">created {formatDate(p.created_at)}</div>
                </div>
              </div>
              <Badge variant={p.role === 'admin' ? 'blue' : 'default'} className="self-start">{roleLabel(p.role)}</Badge>
            </Card>
          </Link>
        ))}
      </div>
      <NameDialog
        open={creating}
        onOpenChange={setCreating}
        title="New project"
        label="Site name"
        submitLabel="Create"
        onSubmit={async (name) => {
          const p = await api.createProject(name)
          qc.invalidateQueries({ queryKey: ['projects'] })
          nav(`/projects/${p.id}/files`)
        }}
      />
    </div>
  )
}
