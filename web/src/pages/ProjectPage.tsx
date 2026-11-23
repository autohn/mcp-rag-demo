import { useState } from 'react'
import { Link, Navigate, NavLink, useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft, FileText, MessageSquareText, MoreHorizontal, Pencil, Trash2, Users } from 'lucide-react'
import { api } from '@/lib/api'
import { cn, roleLabel } from '@/lib/utils'
import {
  Badge, Button, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger, ErrorText, Spinner,
} from '@/components/ui'
import { ConfirmDialog, NameDialog } from '@/components/Dialogs'
import FilesTab from './FilesTab'
import QuestionsTab from './QuestionsTab'
import MembersTab from './MembersTab'

const tabs = [
  { key: 'files', label: 'Files', icon: FileText },
  { key: 'questions', label: 'Questions', icon: MessageSquareText },
  { key: 'members', label: 'Members', icon: Users },
]

export default function ProjectPage() {
  const { id, tab } = useParams()
  const pid = Number(id)
  const project = useQuery({ queryKey: ['project', pid], queryFn: () => api.project(pid) })
  const [renaming, setRenaming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const qc = useQueryClient()
  const nav = useNavigate()

  if (!tab) return <Navigate to={`/projects/${pid}/files`} replace />
  if (project.isLoading) return <Spinner />
  if (project.error || !project.data) return <ErrorText error={project.error ?? 'Project not found'} />
  const p = project.data
  const isAdmin = p.role === 'admin'

  return (
    <div>
      <Link to="/" className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ChevronLeft className="size-4" />All projects
      </Link>
      <div className="mb-4 flex items-center gap-3">
        <h1 className="truncate text-xl font-semibold">{p.name}</h1>
        <Badge variant={isAdmin ? 'blue' : 'default'}>{roleLabel(p.role)}</Badge>
        {isAdmin && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" aria-label="Project actions"><MoreHorizontal /></Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem onSelect={() => setRenaming(true)}><Pencil />Rename project</DropdownMenuItem>
              <DropdownMenuItem destructive onSelect={() => setDeleting(true)}><Trash2 />Delete project</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      <div className="mb-5 flex gap-1 border-b border-border">
        {tabs.map((t) => (
          <NavLink
            key={t.key}
            to={`/projects/${pid}/${t.key}`}
            className={({ isActive }) => cn(
              '-mb-px flex items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium',
              isActive ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground',
            )}
          >
            <t.icon className="size-4" />{t.label}
          </NavLink>
        ))}
      </div>
      {tab === 'files' && <FilesTab project={p} />}
      {tab === 'questions' && <QuestionsTab project={p} />}
      {tab === 'members' && <MembersTab project={p} />}

      <NameDialog open={renaming} onOpenChange={setRenaming} title="Rename project" initial={p.name}
        onSubmit={async (name) => {
          await api.renameProject(pid, name)
          qc.invalidateQueries({ queryKey: ['project', pid] })
          qc.invalidateQueries({ queryKey: ['projects'] })
        }} />
      <ConfirmDialog open={deleting} onOpenChange={setDeleting} title="Delete project?"
        description={<>Project "{p.name}", all its documents and question history will be permanently deleted.</>}
        onConfirm={async () => {
          await api.deleteProject(pid)
          qc.invalidateQueries({ queryKey: ['projects'] })
          nav('/')
        }} />
    </div>
  )
}
