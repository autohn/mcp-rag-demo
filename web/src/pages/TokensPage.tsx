import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, KeyRound, Plus, Trash2 } from 'lucide-react'
import { api, type AccessToken } from '@/lib/api'
import { formatDate } from '@/lib/utils'
import { Button, Card, ErrorText, Input, Spinner } from '@/components/ui'
import { ConfirmDialog } from '@/components/Dialogs'

function CopyButton({ text }: { text: string }) {
  const [done, setDone] = useState(false)
  return (
    <Button
      variant="outline"
      size="sm"
      type="button"
      onClick={async () => {
        await navigator.clipboard.writeText(text)
        setDone(true)
        setTimeout(() => setDone(false), 1500)
      }}
    >
      {done ? <Check /> : <Copy />}{done ? 'Copied' : 'Copy'}
    </Button>
  )
}

function CodeBlock({ code }: { code: string }) {
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded-md bg-slate-900 p-3 pr-28 text-xs leading-relaxed text-slate-100">{code}</pre>
      <div className="absolute right-2 top-2"><CopyButton text={code} /></div>
    </div>
  )
}

export default function TokensPage() {
  const qc = useQueryClient()
  const tokens = useQuery({ queryKey: ['tokens'], queryFn: api.tokens })
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [created, setCreated] = useState<AccessToken | null>(null)
  const [revoking, setRevoking] = useState<AccessToken | null>(null)

  const mcpUrl = `${window.location.origin}/mcp`
  const tok = created?.token ?? '<YOUR_TOKEN>'

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const t = await api.createToken(name)
      setCreated(t)
      setName('')
      qc.invalidateQueries({ queryKey: ['tokens'] })
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="max-w-4xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold">Access Tokens & MCP</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          An Access Token lets an AI assistant (Claude, Cursor, etc.) work with your projects on your behalf, with exactly your rights.
        </p>
      </div>

      <Card className="space-y-4 p-4">
        <form onSubmit={create} className="flex flex-wrap items-end gap-2">
          <div className="min-w-56 flex-1">
            <label htmlFor="t-name" className="mb-1.5 block text-sm font-medium">New token</label>
            <Input id="t-name" placeholder="For example: Claude Desktop on my laptop" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <Button type="submit" loading={busy} disabled={!name.trim()}><Plus />Create token</Button>
        </form>
        <ErrorText error={error} />
        {created?.token && (
          <div className="rounded-md border border-emerald-200 bg-emerald-50 p-3">
            <div className="mb-2 text-sm font-medium text-emerald-800">Token "{created.name}" created. Copy it now — it will not be shown again.</div>
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded bg-white px-2 py-1.5 font-mono text-xs">{created.token}</code>
              <CopyButton text={created.token} />
            </div>
          </div>
        )}
        <div>
          {tokens.isLoading && <Spinner />}
          {tokens.data?.length === 0 && <p className="text-sm text-muted-foreground">No tokens yet.</p>}
          <ul className="divide-y divide-border">
            {tokens.data?.map((t) => (
              <li key={t.id} className="flex items-center gap-3 py-2.5">
                <KeyRound className="size-4 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{t.name}</div>
                  <div className="text-xs text-muted-foreground">
                    created {formatDate(t.created_at)} · {t.last_used_at ? `last used ${formatDate(t.last_used_at)}` : 'never used'}
                  </div>
                </div>
                <Button variant="ghost" size="sm" className="text-destructive" onClick={() => setRevoking(t)}><Trash2 />Revoke</Button>
              </li>
            ))}
          </ul>
        </div>
      </Card>

      <Card className="space-y-5 p-5">
        <div>
          <h2 className="font-semibold">Connecting MCP</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Server URL (Streamable HTTP): <code className="rounded bg-muted px-1.5 py-0.5 text-foreground">{mcpUrl}</code>, authorization via the
            header <code className="rounded bg-muted px-1.5 py-0.5 text-foreground">Authorization: Bearer &lt;token&gt;</code>.
            {!created?.token && ' Create a token and it will be filled into the examples below.'}
          </p>
        </div>

        <section className="space-y-2">
          <h3 className="text-sm font-semibold">Claude Desktop</h3>
          <p className="text-sm text-muted-foreground">Settings → Developer → Edit Config, add to <code>claude_desktop_config.json</code> (requires Node.js):</p>
          <CodeBlock code={JSON.stringify({
            mcpServers: {
              sitedocs: { command: 'npx', args: ['-y', 'mcp-remote', mcpUrl, '--header', `Authorization: Bearer ${tok}`] },
            },
          }, null, 2)} />
        </section>

        <section className="space-y-2">
          <h3 className="text-sm font-semibold">Cursor</h3>
          <p className="text-sm text-muted-foreground">File <code>~/.cursor/mcp.json</code> (or <code>.cursor/mcp.json</code> in a project):</p>
          <CodeBlock code={JSON.stringify({ mcpServers: { sitedocs: { url: mcpUrl, headers: { Authorization: `Bearer ${tok}` } } } }, null, 2)} />
        </section>

        <section className="space-y-2">
          <h3 className="text-sm font-semibold">Claude Code</h3>
          <CodeBlock code={`claude mcp add --transport http sitedocs ${mcpUrl} --header "Authorization: Bearer ${tok}"`} />
        </section>

        <section className="space-y-2">
          <h3 className="text-sm font-semibold">ChatGPT and claude.ai web connectors</h3>
          <p className="text-sm text-muted-foreground">
            These clients connect to remote MCP servers only via OAuth and do not let you set a custom header, so they do not work
            with Access Tokens. OAuth is not implemented in this demo — use Claude Desktop, Cursor or Claude Code.
          </p>
        </section>

        <section className="space-y-1 text-sm">
          <h3 className="font-semibold">What the assistant can do</h3>
          <p className="text-muted-foreground">
            <code>ask_project</code> — an Answer with verified Citations (same as the Questions tab, saved to your history);{' '}
            <code>search_project</code> — raw fragments for its own analysis; plus browsing and managing projects, folders,
            documents and members — within your role.
          </p>
        </section>
      </Card>

      <ConfirmDialog
        open={!!revoking}
        onOpenChange={(v) => !v && setRevoking(null)}
        title="Revoke token?"
        description={revoking && <>Assistants using "{revoking.name}" will lose access immediately.</>}
        confirmLabel="Revoke"
        onConfirm={async () => {
          if (!revoking) return
          await api.revokeToken(revoking.id)
          if (created?.id === revoking.id) setCreated(null)
          qc.invalidateQueries({ queryKey: ['tokens'] })
        }}
      />
    </div>
  )
}
