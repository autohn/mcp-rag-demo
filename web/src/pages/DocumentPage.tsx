import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Document as PdfDocument, Page as PdfPage, pdfjs } from 'react-pdf'
import 'react-pdf/dist/Page/TextLayer.css'
import { ChevronLeft, Download, Quote } from 'lucide-react'
import { api, type Document } from '@/lib/api'
import { findQuote, formatSize } from '@/lib/utils'
import { Button, Card, ErrorText, Spinner } from '@/components/ui'
import { StatusBadge } from './FilesTab'

pdfjs.GlobalWorkerOptions.workerSrc = new URL('pdfjs-dist/build/pdf.worker.min.mjs', import.meta.url).toString()

export default function DocumentPage() {
  const id = Number(useParams().id)
  const [params] = useSearchParams()
  const quote = params.get('quote') ?? ''
  const page = Number(params.get('page') || params.get('block') || 0) || null
  const doc = useQuery({
    queryKey: ['document', id],
    queryFn: () => api.document(id),
    refetchInterval: (q) => (q.state.data?.status === 'processing' ? 1500 : false),
  })

  if (doc.isLoading) return <Spinner />
  if (!doc.data) return <ErrorText error={doc.error ?? 'Document not found'} />
  const d = doc.data
  const back = `/projects/${d.project_id}/files${d.folder_id ? `?folder=${d.folder_id}` : ''}`

  return (
    <div>
      <Link to={back} className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ChevronLeft className="size-4" />Back to project files
      </Link>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <h1 className="min-w-0 truncate text-xl font-semibold">{d.name}</h1>
        <StatusBadge doc={d} />
        <span className="text-sm text-muted-foreground">{d.kind.toUpperCase()} · {formatSize(d.size)}{d.paged && d.page_count ? ` · ${d.page_count} pages` : ''}</span>
        <Button variant="outline" size="sm" className="ml-auto" asChild>
          <a href={api.fileUrl(d.id, true)}><Download />Download</a>
        </Button>
      </div>
      {quote && (
        <Card className="mb-4 flex gap-2 border-amber-200 bg-amber-50 p-3 text-sm">
          <Quote className="mt-0.5 size-4 shrink-0 text-amber-600" />
          <div>
            <div className="text-xs font-medium text-amber-700">Citation{d.paged && page ? `, p. ${page}` : ''}</div>
            <div className="whitespace-pre-line text-slate-800">{quote}</div>
          </div>
        </Card>
      )}
      {d.kind === 'pdf' ? <PdfViewer doc={d} page={page} quote={quote} /> : <TextViewer doc={d} block={page} quote={quote} />}
    </div>
  )
}

// ---- PDF ----

interface TextItemLike { str?: string; hasEOL?: boolean }

/** Indices of text-layer items that overlap the quote on this page. */
function highlightedItems(items: TextItemLike[], quote: string): Set<number> {
  let text = ''
  const owner: number[] = []
  items.forEach((it, i) => {
    const s = (it.str ?? '') + (it.hasEOL ? ' ' : '')
    text += s
    for (let k = 0; k < s.length; k++) owner.push(i)
  })
  const range = findQuote(text, quote)
  const out = new Set<number>()
  if (!range) return out
  for (let k = range[0]; k < range[1]; k++) out.add(owner[k])
  return out
}

const escapeHtml = (s: string) => s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!)

function PdfViewer({ doc, page, quote }: { doc: Document; page: number | null; quote: string }) {
  const [numPages, setNumPages] = useState(0)
  const [error, setError] = useState<unknown>(null)
  const [width, setWidth] = useState(800)
  const box = useRef<HTMLDivElement>(null)
  const file = useMemo(() => ({ url: api.fileUrl(doc.id), withCredentials: true }), [doc.id])

  useEffect(() => {
    const el = box.current
    if (!el) return
    const ro = new ResizeObserver(() => setWidth(Math.min(el.clientWidth - 32, 1000)))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  return (
    <div ref={box} className="rounded-lg border border-border bg-slate-200/60 p-4">
      <ErrorText error={error} />
      <PdfDocument
        file={file}
        onLoadSuccess={(pdf) => setNumPages(pdf.numPages)}
        onLoadError={(e) => setError(new Error('Failed to open PDF: ' + e.message))}
        loading={<div className="p-8"><Spinner /></div>}
      >
        <div className="flex flex-col items-center gap-4">
          {Array.from({ length: numPages }, (_, i) => (
            <LazyPage key={i + 1} n={i + 1} width={width} target={page === i + 1} quote={page === i + 1 ? quote : ''} />
          ))}
        </div>
      </PdfDocument>
    </div>
  )
}

function LazyPage({ n, width, target, quote }: { n: number; width: number; target: boolean; quote: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const [visible, setVisible] = useState(target)
  const [marks, setMarks] = useState<Set<number>>(new Set())
  const scrolled = useRef(false)

  useEffect(() => {
    const el = ref.current
    if (!el || visible) return
    const io = new IntersectionObserver((e) => e[0].isIntersecting && setVisible(true), { rootMargin: '1200px 0px' })
    io.observe(el)
    return () => io.disconnect()
  }, [visible])

  useEffect(() => {
    if (target && !scrolled.current) ref.current?.scrollIntoView({ block: 'start' })
  }, [target])

  const renderer = useCallback(
    ({ str, itemIndex }: { str: string; itemIndex: number }) => (marks.has(itemIndex) ? `<mark>${escapeHtml(str)}</mark>` : escapeHtml(str)),
    [marks],
  )

  return (
    <div ref={ref} id={`page-${n}`} className="relative bg-white shadow" style={{ minHeight: visible ? undefined : width * 1.414, width }}>
      {visible && (
        <PdfPage
          pageNumber={n}
          width={width}
          renderAnnotationLayer={false}
          customTextRenderer={quote ? renderer : undefined}
          onGetTextSuccess={(tc) => quote && setMarks(highlightedItems(tc.items as TextItemLike[], quote))}
          onRenderTextLayerSuccess={() => {
            if (target && !scrolled.current) {
              scrolled.current = true
              const mark = ref.current?.querySelector('mark')
              ;(mark ?? ref.current)?.scrollIntoView({ block: mark ? 'center' : 'start' })
            }
          }}
          loading={<div style={{ height: width * 1.414 }} />}
        />
      )}
      <div className="absolute -left-1 top-1 -translate-x-full pr-2 text-xs text-muted-foreground">{n}</div>
    </div>
  )
}

// ---- DOCX / TXT / MD ----

function TextViewer({ doc, block, quote }: { doc: Document; block: number | null; quote: string }) {
  const pages = useQuery({ queryKey: ['pages', doc.id, doc.status], queryFn: () => api.pages(doc.id), enabled: doc.status === 'ready' })
  const markRef = useRef<HTMLElement>(null)

  useEffect(() => {
    markRef.current?.scrollIntoView({ block: 'center' })
  }, [pages.data])

  if (doc.status !== 'ready') return <Card className="p-6 text-sm text-muted-foreground">Text will appear after indexing.</Card>
  if (pages.isLoading) return <Spinner />
  if (!pages.data?.length) return <Card className="p-6 text-sm text-muted-foreground">The document has no text.</Card>

  // Highlight in the cited block, or in the first block that contains the quote.
  let hit: { page: number; range: [number, number] } | null = null
  if (quote) {
    const order = [...pages.data].sort((a, b) => (a.page === block ? -1 : b.page === block ? 1 : 0))
    for (const p of order) {
      const r = findQuote(p.text, quote)
      if (r) { hit = { page: p.page, range: r }; break }
    }
  }

  return (
    <Card className="mx-auto max-w-4xl p-8">
      <div className="space-y-6 text-[15px] leading-relaxed">
        {pages.data.map((p) => (
          <div key={p.page} className="whitespace-pre-wrap">
            {hit && hit.page === p.page ? (
              <>
                {p.text.slice(0, hit.range[0])}
                <mark ref={markRef} className="rounded bg-yellow-200 px-0.5">{p.text.slice(hit.range[0], hit.range[1])}</mark>
                {p.text.slice(hit.range[1])}
              </>
            ) : p.text}
          </div>
        ))}
      </div>
    </Card>
  )
}
