import type { Claw, ClawStatus } from "@/lib/types"

/**
 * Elapsed time helpers for showing how long a run has been going.
 *
 * The card used to show `claw.uptime`, which had three problems for someone
 * watching a run: it was rendered as whole minutes so a task sat on "3m" for sixty
 * seconds, it was only recalculated when hub data arrived rather than ticking, and
 * it was defined as zero unless the claw was connected or idle — so a finished run
 * showed "—" and there was no way to see how long it had taken.
 */

/** Statuses where the clock is still running. */
const ACTIVE_STATUSES: ReadonlySet<ClawStatus> = new Set<ClawStatus>([
  "provisioning",
  "connected",
  "idle",
])

export function isClawRunning(status: ClawStatus): boolean {
  return ACTIVE_STATUSES.has(status)
}

/**
 * elapsedSeconds returns how long the run has been going, or how long it took if
 * it has finished.
 *
 * A finished run is measured to last_seen rather than to now, so the number stops
 * climbing and becomes the run's duration. Without that the display would keep
 * counting up on a claw that stopped an hour ago.
 */
export function elapsedSeconds(claw: Claw, nowMs: number): number | null {
  if (!claw.created_at) return null
  const created = new Date(claw.created_at).getTime()
  if (!Number.isFinite(created)) return null

  let end = nowMs
  if (!isClawRunning(claw.status)) {
    if (!claw.last_seen) return null
    const lastSeen = new Date(claw.last_seen).getTime()
    if (!Number.isFinite(lastSeen)) return null
    end = lastSeen
  }
  // Clock skew between the hub's timestamps and the browser can make a run that
  // just started look negative, which would render as "-0:01".
  return Math.max(0, Math.floor((end - created) / 1000))
}

/**
 * formatElapsed renders seconds as m:ss, or h:mm:ss past an hour.
 *
 * Seconds are always shown. The point of this display is to watch a run progress,
 * and a counter that only moves once a minute cannot be distinguished from one that
 * has frozen — which is exactly the question being asked when someone looks at it.
 */
export function formatElapsed(seconds: number | null): string {
  if (seconds === null) return "—"
  const s = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(s / 3600)
  const mins = Math.floor((s % 3600) / 60)
  const secs = s % 60
  const pad = (n: number) => n.toString().padStart(2, "0")
  return hours > 0 ? `${hours}:${pad(mins)}:${pad(secs)}` : `${mins}:${pad(secs)}`
}
