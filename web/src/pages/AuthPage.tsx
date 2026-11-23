import { useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Building2 } from 'lucide-react'
import { api } from '@/lib/api'
import { Button, Card, ErrorText, Input, Label } from '@/components/ui'

export default function AuthPage({ mode }: { mode: 'login' | 'register' }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const qc = useQueryClient()
  const nav = useNavigate()
  const from = (useLocation().state as { from?: string } | null)?.from ?? '/'
  const isLogin = mode === 'login'

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const user = isLogin ? await api.login(email, password) : await api.register(email, password, name)
      qc.setQueryData(['me'], user)
      nav(from, { replace: true })
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center gap-2 text-center">
          <span className="flex size-10 items-center justify-center rounded-lg bg-primary text-white"><Building2 className="size-5" /></span>
          <h1 className="text-xl font-semibold">SiteDocs</h1>
          <p className="text-sm text-muted-foreground">Construction site documentation with cited answers</p>
        </div>
        <Card className="p-6">
          <h2 className="mb-4 text-base font-semibold">{isLogin ? 'Sign in' : 'Create account'}</h2>
          <form onSubmit={submit} className="space-y-4">
            {!isLogin && (
              <div className="space-y-1.5">
                <Label htmlFor="name">Name</Label>
                <Input id="name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Jane Smith" />
              </div>
            )}
            <div className="space-y-1.5">
              <Label htmlFor="email">Email</Label>
              <Input id="email" type="email" required autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="password">Password</Label>
              <Input id="password" type="password" required minLength={isLogin ? undefined : 6}
                autoComplete={isLogin ? 'current-password' : 'new-password'} value={password} onChange={(e) => setPassword(e.target.value)} />
            </div>
            <ErrorText error={error} />
            <Button type="submit" className="w-full" loading={busy}>{isLogin ? 'Sign in' : 'Create account'}</Button>
          </form>
          <p className="mt-4 text-center text-sm text-muted-foreground">
            {isLogin ? <>No account? <Link className="text-primary hover:underline" to="/register">Create one</Link></>
              : <>Already have an account? <Link className="text-primary hover:underline" to="/login">Sign in</Link></>}
          </p>
        </Card>
        {isLogin && (
          <p className="mt-4 text-center text-xs text-muted-foreground">
            Demo: admin@demo.local or prorab@demo.local, password demo1234
          </p>
        )}
      </div>
    </div>
  )
}
