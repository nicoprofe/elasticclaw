"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { getHubUrl } from "@/lib/hub-url"
import { requestAuthToken } from "@/lib/auth-storage"

// Shapes are a subset of what the settings page uses for the same endpoints.
interface ModelAuthProfile {
  name: string
  provider: string
  mode: string
  authenticated: boolean
}

interface ModelAuthLoginJob {
  id: string
  status: string
  url?: string
  code?: string
  error?: string
}

const CODEX_PROVIDER = "codex"

/**
 * CodexConnectBanner asks the user to connect Codex, and disappears once they
 * have. Connecting is otherwise buried in Settings behind provider and profile
 * choices that mean nothing to someone who is not a developer, so this surfaces
 * the one action that matters: open a link, approve, done.
 */
export function CodexConnectBanner() {
  const [connected, setConnected] = useState<boolean | null>(null)
  // A profile that exists but is not authenticated means the sign-in expired,
  // which needs different wording from having never connected at all.
  const [expired, setExpired] = useState(false)
  const [job, setJob] = useState<ModelAuthLoginJob | null>(null)
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState("")
  const [copied, setCopied] = useState(false)
  const [dismissed, setDismissed] = useState(false)
  const pollRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const authHeaders = useCallback(async (): Promise<Record<string, string> | null> => {
    const token = await requestAuthToken()
    if (!token) return null
    return { Authorization: `Bearer ${token}` }
  }, [])

  // Is Codex already connected? Until we know, render nothing rather than
  // flashing a "connect" prompt at someone who already has.
  const refreshStatus = useCallback(async () => {
    const headers = await authHeaders()
    if (!headers) return
    try {
      const res = await fetch(`${getHubUrl()}/api/settings`, { headers })
      if (!res.ok) return
      const settings = await res.json()
      const profiles: ModelAuthProfile[] = settings?.modelAuthProfiles ?? []
      const codexProfiles = profiles.filter((p) => p.provider === CODEX_PROVIDER)
      setConnected(codexProfiles.some((p) => p.authenticated))
      setExpired(codexProfiles.length > 0 && !codexProfiles.some((p) => p.authenticated))
    } catch {
      // Offline or unauthorized: stay quiet instead of showing a broken prompt.
    }
  }, [authHeaders])

  useEffect(() => {
    void refreshStatus()
  }, [refreshStatus])

  // Poll the device-login job until it finishes, then re-check the real status
  // rather than trusting the job's own terminal state.
  useEffect(() => {
    if (!job || job.status !== "running") return
    let cancelled = false

    const tick = async () => {
      const headers = await authHeaders()
      if (!headers || cancelled) return
      try {
        const res = await fetch(
          `${getHubUrl()}/api/settings/model-auth/login/${encodeURIComponent(job.id)}`,
          { headers },
        )
        if (!res.ok) return
        const next: ModelAuthLoginJob = await res.json()
        if (cancelled) return
        setJob(next)
        if (next.status !== "running") {
          if (next.error) setError(next.error)
          void refreshStatus()
          return
        }
      } catch {
        // Transient failure: the next tick retries.
      }
      if (!cancelled) pollRef.current = setTimeout(tick, 2000)
    }

    pollRef.current = setTimeout(tick, 2000)
    return () => {
      cancelled = true
      if (pollRef.current) clearTimeout(pollRef.current)
    }
  }, [job, authHeaders, refreshStatus])

  const startConnect = async () => {
    setError("")
    setStarting(true)
    try {
      const headers = await authHeaders()
      if (!headers) {
        setError("You need to sign in again before connecting Codex.")
        return
      }
      const res = await fetch(`${getHubUrl()}/api/settings/model-auth/login`, {
        method: "POST",
        headers: { ...headers, "Content-Type": "application/json" },
        body: JSON.stringify({ provider: CODEX_PROVIDER, profile: "codex-default", mode: "device" }),
      })
      if (!res.ok) throw new Error(await res.text())
      setJob(await res.json())
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not start the Codex sign-in.")
    } finally {
      setStarting(false)
    }
  }

  const copyCode = async () => {
    if (!job?.code) return
    try {
      await navigator.clipboard.writeText(job.code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard permission denied; the code is on screen to type manually.
    }
  }

  // Nothing to say while status is unknown, once connected, or if dismissed.
  if (connected === null || connected || dismissed) return null

  const waiting = job?.status === "running"

  return (
    <div className="border-b border-border bg-muted/40 px-4 py-3">
      <div className="mx-auto flex max-w-4xl flex-col gap-3">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <p className="text-sm font-medium text-foreground">
              {expired ? "Your Codex sign-in expired" : "Connect Codex to start running agents"}
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              {expired
                ? "Agents cannot edit your repositories until you sign in to Codex again. Reconnecting takes about a minute."
                : "Agents need your Codex account before they can write code. This takes about a minute and only has to be done once."}
            </p>
          </div>
          {!waiting && (
            <Button size="sm" onClick={startConnect} disabled={starting}>
              {starting ? "Starting…" : expired ? "Reconnect Codex" : "Connect Codex"}
            </Button>
          )}
          {waiting && (
            <button
              type="button"
              onClick={() => setDismissed(true)}
              className="shrink-0 text-sm text-muted-foreground underline-offset-4 hover:underline"
            >
              Hide
            </button>
          )}
        </div>

        {waiting && job?.url && (
          <div className="rounded-md border border-border bg-background p-3">
            <p className="text-sm text-foreground">1. Open:</p>
            {/* The URL is the link text so it is obvious where it goes and can be
                copied or typed into another browser if the click is blocked. */}
            <a
              href={job.url}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-1 block break-all font-mono text-sm font-medium text-primary underline underline-offset-4"
            >
              {job.url}
            </a>
            {job.code && (
              <p className="mt-3 flex flex-wrap items-center gap-2 text-sm text-foreground">
                2. Enter code:
                <code className="rounded bg-muted px-2 py-1 font-mono text-sm tracking-widest">{job.code}</code>
                <button
                  type="button"
                  onClick={copyCode}
                  className="text-sm text-muted-foreground underline-offset-4 hover:underline"
                >
                  {copied ? "Copied" : "Copy"}
                </button>
              </p>
            )}
            <p className="mt-2 text-sm text-muted-foreground">
              Waiting for you to finish in the browser… this banner disappears once you are connected.
            </p>
          </div>
        )}

        {waiting && !job?.url && (
          <p className="text-sm text-muted-foreground">Preparing your sign-in link…</p>
        )}

        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
    </div>
  )
}
