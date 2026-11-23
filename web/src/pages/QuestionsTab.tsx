import { Fragment, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, FileText, History, Quote, SearchX, Send } from 'lucide-react'
import { api, type Citation, type Project, type QuestionRecord } from '@/lib/api'
import { cn, formatDate } from '@/lib/utils'
import { Badge, Button, Card, ErrorText, Select, Spinner, Textarea } from '@/components/ui'
import { flattenFolders } from './FilesTab'

export function citationHref(c: Citation) {
  const p = new URLSearchParams({ quote: c.quote })
  if (c.paged) p.set('page', String(c.page))
  else p.set('block', String(c.page))
  return `/documents/${c.document_id}?${p}`
}

export default function QuestionsTab({ project }: { project: Project }) {
  const pid = project.id
  const qc = useQueryClient()
  const config = useQuery({ queryKey: ['config'], queryFn: api.config, staleTime: Infinity })
  const folders = useQuery({ queryKey: ['folders', pid], queryFn: () => api.allFolders(pid) })
  const history = useQuery({ queryKey: ['history', pid], queryFn: () => api.history(pid) })
  const tree = useMemo(() => flattenFolders(folders.data ?? []), [folders.data])

  const [question, setQuestion] = useState('')
  const [folder, setFolder] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [selected, setSelected] = useState<number | null>(null)

  const current = history.data?.find((q) => q.id === selected)
  const folderName = (id: number | null) => (id == null ? null : folders.data?.find((f) => f.id === id)?.name ?? 'deleted folder')

  const ask = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!question.trim()) return
    setBusy(true)
    setError(null)
    try {
      const rec = await api.ask(pid, question, folder ? Number(folder) : null)
      qc.setQueryData<QuestionRecord[]>(['history', pid], (old) => [rec, ...(old ?? [])])
      setSelected(rec.id)
      setQuestion('')
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-5 lg:grid-cols-[1fr_320px]">
      <div className="min-w-0 space-y-4">
        {config.data && !config.data.llm_enabled && (
          <Card className="flex gap-3 border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">
            <AlertTriangle className="size-5 shrink-0" />
            OPENAI_API_KEY is not set on the server, so Answer generation is disabled. Uploads and full-text search still work.
          </Card>
        )}
        <Card className="p-4">
          <form onSubmit={ask} className="space-y-3">
            <Textarea
              value={question}
              onChange={(e) => setQuestion(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) ask(e) }}
              placeholder="For example: what concrete grade is used for the columns from the 5th floor up?"
              className="min-h-24 text-base"
            />
            <div className="flex flex-wrap items-center gap-2">
              <Select value={folder} onChange={(e) => setFolder(e.target.value)} className="w-auto max-w-xs" aria-label="Search in folder">
                <option value="">Whole project</option>
                {tree.map(({ folder: f, depth }) => (
                  <option key={f.id} value={f.id}>{'  '.repeat(depth)}Folder: {f.name}</option>
                ))}
              </Select>
              <span className="hidden text-xs text-muted-foreground sm:inline">Ctrl+Enter to send. Each question stands alone.</span>
              <Button type="submit" className="ml-auto" loading={busy} disabled={!question.trim()}><Send />Ask</Button>
            </div>
            <ErrorText error={error} />
          </form>
        </Card>

        {busy && (
          <Card className="flex items-center gap-3 p-4 text-sm text-muted-foreground"><Spinner />Searching the project documents…</Card>
        )}
        {!busy && current && <AnswerCard rec={current} folderName={folderName(current.folder_id)} />}
        {!busy && !current && (
          <Card className="p-8 text-center text-sm text-muted-foreground">
            Ask a question about the project documents. Answers come only from uploaded documents, and every claim cites its page.
          </Card>
        )}
      </div>

      <Card className="h-fit p-2">
        <div className="flex items-center gap-2 px-2 py-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          <History className="size-4" />My history
        </div>
        {history.isLoading && <div className="p-2"><Spinner /></div>}
        {history.data?.length === 0 && <p className="px-2 py-3 text-sm text-muted-foreground">No questions yet.</p>}
        <div className="max-h-[70vh] overflow-y-auto">
          {history.data?.map((q) => (
            <button
              key={q.id}
              onClick={() => setSelected(q.id)}
              className={cn('block w-full rounded-md px-2 py-2 text-left hover:bg-muted', selected === q.id && 'bg-accent')}
            >
              <div className="line-clamp-2 text-sm">{q.question}</div>
              <div className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
                {formatDate(q.created_at)}
                {!q.found && <Badge variant="default">not found</Badge>}
              </div>
            </button>
          ))}
        </div>
      </Card>
    </div>
  )
}

function AnswerCard({ rec, folderName }: { rec: QuestionRecord; folderName: string | null }) {
  const [active, setActive] = useState<number | null>(null)
  const parts = rec.answer.split(/(\[\d+\])/g)
  return (
    <Card className="p-5">
      <div className="mb-1 text-xs text-muted-foreground">
        {formatDate(rec.created_at)}{folderName && <> · in folder "{folderName}"</>}
      </div>
      <h2 className="mb-3 font-semibold">{rec.question}</h2>
      {rec.found ? (
        <p className="whitespace-pre-wrap leading-relaxed">
          {parts.map((part, i) => {
            const m = part.match(/^\[(\d+)\]$/)
            if (!m) return <Fragment key={i}>{part}</Fragment>
            const n = Number(m[1])
            return (
              <a
                key={i}
                href={`#cit-${rec.id}-${n}`}
                onMouseEnter={() => setActive(n)}
                onMouseLeave={() => setActive(null)}
                className="mx-0.5 inline-flex h-5 min-w-5 -translate-y-0.5 items-center justify-center rounded bg-blue-100 px-1 text-xs font-semibold text-blue-700 hover:bg-blue-200"
              >
                {n}
              </a>
            )
          })}
        </p>
      ) : (
        <div className="flex items-center gap-2 text-muted-foreground"><SearchX className="size-5" />{rec.answer}</div>
      )}

      {rec.citations.length > 0 && (
        <div className="mt-5 space-y-2">
          <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Citations</div>
          {rec.citations.map((c) => (
            <div
              key={c.n}
              id={`cit-${rec.id}-${c.n}`}
              className={cn('flex gap-3 rounded-md border border-border p-3 transition-colors', active === c.n && 'border-blue-300 bg-blue-50')}
            >
              <span className="flex size-6 shrink-0 items-center justify-center rounded bg-blue-100 text-xs font-semibold text-blue-700">{c.n}</span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  {c.document_deleted ? (
                    <span className="flex items-center gap-1.5 text-muted-foreground line-through"><FileText className="size-4" />{c.document_name}</span>
                  ) : (
                    <Link to={citationHref(c)} className="flex items-center gap-1.5 font-medium text-primary hover:underline">
                      <FileText className="size-4" />{c.document_name}
                    </Link>
                  )}
                  {c.paged && <span className="text-muted-foreground">p. {c.page}</span>}
                  {c.document_deleted && <Badge variant="red">document deleted</Badge>}
                </div>
                <blockquote className="mt-1.5 flex gap-2 text-sm text-slate-700">
                  <Quote className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                  <span className="whitespace-pre-line">{c.quote}</span>
                </blockquote>
              </div>
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}
