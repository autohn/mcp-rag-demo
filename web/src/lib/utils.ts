import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

const dtf = new Intl.DateTimeFormat('en-US', { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })
export const formatDate = (s: string) => dtf.format(new Date(s))

export const roleLabel = (r: string) => (r === 'admin' ? 'Admin' : 'Participant')

/**
 * Normalization shared with the backend citation check: case, ё, quotes, dashes, whitespace.
 * Returns the normalized string and, for each of its chars, the index in the source.
 */
export function normalize(s: string): { text: string; map: number[] } {
  let out = ''
  const map: number[] = []
  let prevSpace = true
  for (let i = 0; i < s.length; i++) {
    let c = s[i]
    if (c === '­') continue
    if (/\s/.test(c)) {
      if (prevSpace) continue
      c = ' '
      prevSpace = true
    } else {
      prevSpace = false
      c = c.toLowerCase()
      if (c === 'ё') c = 'е'
      else if ('«»“”„″'.includes(c)) c = '"'
      else if ('‘’′'.includes(c)) c = "'"
      else if ('–—‑−'.includes(c)) c = '-'
    }
    out += c
    map.push(i)
  }
  return { text: out, map }
}

/** Finds quote in text (normalized) and returns the [start, end) range in the source text. */
export function findQuote(text: string, quote: string): [number, number] | null {
  const q = normalize(quote).text.trim()
  if (q.length < 3) return null
  const t = normalize(text)
  const pos = t.text.indexOf(q)
  if (pos < 0) return null
  return [t.map[pos], t.map[pos + q.length - 1] + 1]
}
