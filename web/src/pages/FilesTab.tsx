import { useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle, ChevronRight, Download, File as FileIcon, FileText, Folder as FolderIcon, FolderInput, FolderOpen,
  FolderPlus, Home, Loader2, MoreHorizontal, Pencil, Trash2, Upload,
} from 'lucide-react'
import { api, type Document, type Folder, type Project } from '@/lib/api'
import { cn, formatDate, formatSize } from '@/lib/utils'
import {
  Badge, Button, Card, Dialog, DialogContent, DropdownMenu, DropdownMenuContent, DropdownMenuItem,
  DropdownMenuSeparator, DropdownMenuTrigger, ErrorText, Select, Spinner,
} from '@/components/ui'
import { ConfirmDialog, NameDialog } from '@/components/Dialogs'

const ACCEPT = '.pdf,.docx,.txt,.md'

/** Flattens the folder tree depth-first, for the sidebar and folder pickers. */
export function flattenFolders(folders: Folder[]) {
  const children = new Map<number | null, Folder[]>()
  for (const f of folders) {
    const list = children.get(f.parent_id) ?? []
    list.push(f)
    children.set(f.parent_id, list)
  }
  const out: { folder: Folder; depth: number }[] = []
  const walk = (parent: number | null, depth: number) => {
    for (const f of (children.get(parent) ?? []).sort((a, b) => a.name.localeCompare(b.name, 'ru'))) {
      out.push({ folder: f, depth })
      walk(f.id, depth + 1)
    }
  }
  walk(null, 0)
  return out
}

function subtreeIds(folders: Folder[], root: number) {
  const ids = new Set([root])
  let grew = true
  while (grew) {
    grew = false
    for (const f of folders) {
      if (f.parent_id != null && ids.has(f.parent_id) && !ids.has(f.id)) {
        ids.add(f.id)
        grew = true
      }
    }
  }
  return ids
}

export function StatusBadge({ doc }: { doc: Document }) {
  if (doc.status === 'processing') return <Badge variant="amber"><Loader2 className="size-3 animate-spin" />Indexing</Badge>
  if (doc.status === 'failed') return <Badge variant="red" title={doc.error}><AlertCircle className="size-3" />Failed</Badge>
  if (doc.no_text) return <Badge variant="default" title="The file has no text layer, so it is not searchable">No text</Badge>
  return <Badge variant="green">Ready</Badge>
}

type Item = { type: 'folder'; folder: Folder } | { type: 'document'; doc: Document }

export default function FilesTab({ project }: { project: Project }) {
  const pid = project.id
  const isAdmin = project.role === 'admin'
  const [params, setParams] = useSearchParams()
  const folderId = params.get('folder') ? Number(params.get('folder')) : null
  const setFolder = (id: number | null) => setParams(id == null ? {} : { folder: String(id) })
  const qc = useQueryClient()

  const allFolders = useQuery({ queryKey: ['folders', pid], queryFn: () => api.allFolders(pid) })
  const listing = useQuery({
    queryKey: ['listing', pid, folderId],
    queryFn: () => api.listing(pid, folderId),
    refetchInterval: (q) => (q.state.data?.documents.some((d) => d.status === 'processing') ? 1500 : false),
  })
  const tree = useMemo(() => flattenFolders(allFolders.data ?? []), [allFolders.data])

  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState<unknown>(null)
  const [dragOver, setDragOver] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)

  const [newFolder, setNewFolder] = useState(false)
  const [renaming, setRenaming] = useState<Item | null>(null)
  const [moving, setMoving] = useState<Item | null>(null)
  const [deleting, setDeleting] = useState<Item | null>(null)

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['folders', pid] })
    qc.invalidateQueries({ queryKey: ['listing', pid] })
  }

  const upload = async (files: File[]) => {
    if (!isAdmin || files.length === 0) return
    setUploading(true)
    setUploadError(null)
    try {
      await api.upload(pid, folderId, files)
    } catch (e) {
      setUploadError(e)
    } finally {
      setUploading(false)
      refresh()
    }
  }

  const itemName = (i: Item) => (i.type === 'folder' ? i.folder.name : i.doc.name)

  return (
    <div className="grid gap-5 lg:grid-cols-[260px_1fr]">
      <Card className="h-fit p-2">
        <div className="flex items-center justify-between px-2 py-1.5">
          <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Folders</span>
          {isAdmin && (
            <Button variant="ghost" size="icon" onClick={() => setNewFolder(true)} aria-label="New folder" title="New folder">
              <FolderPlus />
            </Button>
          )}
        </div>
        <button
          onClick={() => setFolder(null)}
          className={cn('flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted', folderId == null && 'bg-accent font-medium text-primary')}
        >
          <Home className="size-4 shrink-0" />Project root
        </button>
        {tree.map(({ folder, depth }) => (
          <button
            key={folder.id}
            onClick={() => setFolder(folder.id)}
            style={{ paddingLeft: 8 + (depth + 1) * 14 }}
            className={cn('flex w-full items-center gap-2 rounded-md py-1.5 pr-2 text-left text-sm hover:bg-muted', folderId === folder.id && 'bg-accent font-medium text-primary')}
          >
            {folderId === folder.id ? <FolderOpen className="size-4 shrink-0" /> : <FolderIcon className="size-4 shrink-0" />}
            <span className="truncate">{folder.name}</span>
          </button>
        ))}
      </Card>

      <div
        className="min-w-0"
        onDragOver={(e) => { if (isAdmin) { e.preventDefault(); setDragOver(true) } }}
        onDragLeave={(e) => { if (e.currentTarget === e.target) setDragOver(false) }}
        onDrop={(e) => {
          e.preventDefault()
          setDragOver(false)
          upload(Array.from(e.dataTransfer.files))
        }}
      >
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <nav className="flex min-w-0 flex-1 items-center gap-1 text-sm">
            <button className="text-muted-foreground hover:text-foreground" onClick={() => setFolder(null)}>Root</button>
            {listing.data?.path.map((f) => (
              <span key={f.id} className="flex min-w-0 items-center gap-1">
                <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
                <button className="truncate hover:underline" onClick={() => setFolder(f.id)}>{f.name}</button>
              </span>
            ))}
          </nav>
          {isAdmin && (
            <>
              <Button variant="outline" size="sm" onClick={() => setNewFolder(true)}><FolderPlus />Folder</Button>
              <Button size="sm" loading={uploading} onClick={() => fileInput.current?.click()}><Upload />Upload</Button>
              <input ref={fileInput} type="file" multiple accept={ACCEPT} className="hidden"
                onChange={(e) => { upload(Array.from(e.target.files ?? [])); e.target.value = '' }} />
            </>
          )}
        </div>
        <ErrorText error={uploadError} />

        <Card className={cn('relative overflow-hidden', dragOver && 'ring-2 ring-primary')}>
          {dragOver && (
            <div className="absolute inset-0 z-10 flex items-center justify-center bg-accent/90 text-sm font-medium text-primary">
              Drop files to upload them to this folder
            </div>
          )}
          {listing.isLoading ? (
            <div className="p-6"><Spinner /></div>
          ) : listing.error ? (
            <div className="p-6"><ErrorText error={listing.error} /></div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="border-b border-border bg-slate-50 text-left text-xs text-muted-foreground">
                  <tr>
                    <th className="px-4 py-2 font-medium">Name</th>
                    <th className="px-3 py-2 font-medium">Type</th>
                    <th className="px-3 py-2 font-medium">Size</th>
                    <th className="px-3 py-2 font-medium">Uploaded by</th>
                    <th className="px-3 py-2 font-medium">Status</th>
                    <th className="w-10" />
                  </tr>
                </thead>
                <tbody>
                  {listing.data!.folders.map((f) => (
                    <tr key={`f${f.id}`} className="border-b border-border last:border-0 hover:bg-slate-50">
                      <td className="px-4 py-2">
                        <button className="flex items-center gap-2 font-medium" onClick={() => setFolder(f.id)}>
                          <FolderIcon className="size-4 text-amber-500" />{f.name}
                        </button>
                      </td>
                      <td className="px-3 py-2 text-muted-foreground">Folder</td>
                      <td /><td /><td />
                      <td className="pr-2">
                        {isAdmin && (
                          <RowMenu
                            onRename={() => setRenaming({ type: 'folder', folder: f })}
                            onMove={() => setMoving({ type: 'folder', folder: f })}
                            onDelete={() => setDeleting({ type: 'folder', folder: f })}
                          />
                        )}
                      </td>
                    </tr>
                  ))}
                  {listing.data!.documents.map((d) => (
                    <tr key={`d${d.id}`} className="border-b border-border last:border-0 hover:bg-slate-50">
                      <td className="max-w-0 px-4 py-2">
                        <Link to={`/documents/${d.id}`} className="flex items-center gap-2 hover:text-primary">
                          {d.kind === 'pdf' ? <FileText className="size-4 shrink-0 text-red-500" /> : <FileIcon className="size-4 shrink-0 text-blue-500" />}
                          <span className="truncate">{d.name}</span>
                        </Link>
                      </td>
                      <td className="px-3 py-2 uppercase text-muted-foreground">{d.kind}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">{formatSize(d.size)}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">
                        <div>{d.uploaded_by_name || '—'}</div>
                        <div className="text-xs">{formatDate(d.created_at)}</div>
                      </td>
                      <td className="px-3 py-2">
                        <StatusBadge doc={d} />
                        {d.status === 'failed' && <div className="mt-1 max-w-56 text-xs text-destructive">{d.error}</div>}
                      </td>
                      <td className="pr-2">
                        <RowMenu
                          download={api.fileUrl(d.id, true)}
                          onRename={isAdmin ? () => setRenaming({ type: 'document', doc: d }) : undefined}
                          onMove={isAdmin ? () => setMoving({ type: 'document', doc: d }) : undefined}
                          onDelete={isAdmin ? () => setDeleting({ type: 'document', doc: d }) : undefined}
                        />
                      </td>
                    </tr>
                  ))}
                  {listing.data!.folders.length === 0 && listing.data!.documents.length === 0 && (
                    <tr>
                      <td colSpan={6} className="px-4 py-12 text-center text-muted-foreground">
                        {isAdmin ? 'This folder is empty. Drop PDF, DOCX, TXT or MD files here (up to 50 MB).' : 'This folder is empty.'}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </Card>
        {isAdmin && <p className="mt-2 text-xs text-muted-foreground">PDF with a text layer, DOCX, TXT, MD — up to 50 MB. Scans without text are not indexed.</p>}
      </div>

      <NameDialog open={newFolder} onOpenChange={setNewFolder} title="New folder" submitLabel="Create"
        onSubmit={async (name) => { await api.createFolder(pid, name, folderId); refresh() }} />
      <NameDialog
        open={!!renaming}
        onOpenChange={(v) => !v && setRenaming(null)}
        title={renaming?.type === 'folder' ? 'Rename folder' : 'Rename document'}
        initial={renaming ? itemName(renaming) : ''}
        onSubmit={async (name) => {
          if (!renaming) return
          if (renaming.type === 'folder') await api.renameFolder(renaming.folder.id, name)
          else await api.renameDocument(renaming.doc.id, name)
          refresh()
        }}
      />
      <MoveDialog
        item={moving}
        folders={allFolders.data ?? []}
        onClose={() => setMoving(null)}
        onMoved={refresh}
      />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(v) => !v && setDeleting(null)}
        title={deleting?.type === 'folder' ? 'Delete folder?' : 'Delete document?'}
        description={deleting?.type === 'folder'
          ? <>Folder "{deleting.folder.name}" will be deleted with all its subfolders and documents. This cannot be undone.</>
          : <>Document "{deleting && itemName(deleting)}" will be deleted and immediately stop being used in answers. Citations in history are kept and marked "document deleted".</>}
        onConfirm={async () => {
          if (!deleting) return
          if (deleting.type === 'folder') {
            await api.deleteFolder(deleting.folder.id)
            if (folderId != null && subtreeIds(allFolders.data ?? [], deleting.folder.id).has(folderId)) setFolder(null)
          } else await api.deleteDocument(deleting.doc.id)
          refresh()
        }}
      />
    </div>
  )
}

function RowMenu({ download, onRename, onMove, onDelete }: {
  download?: string
  onRename?: () => void
  onMove?: () => void
  onDelete?: () => void
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="Actions"><MoreHorizontal /></Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        {download && <DropdownMenuItem asChild><a href={download}><Download />Download</a></DropdownMenuItem>}
        {onRename && <DropdownMenuItem onSelect={onRename}><Pencil />Rename</DropdownMenuItem>}
        {onMove && <DropdownMenuItem onSelect={onMove}><FolderInput />Move</DropdownMenuItem>}
        {onDelete && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem destructive onSelect={onDelete}><Trash2 />Delete</DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function MoveDialog({ item, folders, onClose, onMoved }: {
  item: Item | null
  folders: Folder[]
  onClose: () => void
  onMoved: () => void
}) {
  const [target, setTarget] = useState('root')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const excluded = item?.type === 'folder' ? subtreeIds(folders, item.folder.id) : new Set<number>()
  const options = flattenFolders(folders).filter(({ folder }) => !excluded.has(folder.id))

  return (
    <Dialog open={!!item} onOpenChange={(v) => { if (!v) { onClose(); setError(null); setTarget('root') } }}>
      <DialogContent title="Move" description={item && (item.type === 'folder' ? `Folder "${item.folder.name}"` : `Document "${item.doc.name}"`)}>
        <Select value={target} onChange={(e) => setTarget(e.target.value)}>
          <option value="root">Project root</option>
          {options.map(({ folder, depth }) => (
            <option key={folder.id} value={folder.id}>{'  '.repeat(depth) + folder.name}</option>
          ))}
        </Select>
        <div className="mt-3"><ErrorText error={error} /></div>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button
            loading={busy}
            onClick={async () => {
              if (!item) return
              setBusy(true)
              setError(null)
              const dest = target === 'root' ? null : Number(target)
              try {
                if (item.type === 'folder') await api.moveFolder(item.folder.id, dest)
                else await api.moveDocument(item.doc.id, dest)
                onMoved()
                onClose()
                setTarget('root')
              } catch (e) {
                setError(e)
              } finally {
                setBusy(false)
              }
            }}
          >
            Move
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
