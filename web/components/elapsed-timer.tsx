"use client"

import { useNow } from "@/hooks/use-ticker"
import { elapsedSeconds, formatElapsed, isClawRunning } from "@/lib/elapsed"
import type { Claw } from "@/lib/types"
import { cn } from "@/lib/utils"

/**
 * ElapsedTimer shows how long a run has been going, counting up every second, and
 * freezes at the total once the run ends.
 */
export function ElapsedTimer({ claw, className }: { claw: Claw; className?: string }) {
  const running = isClawRunning(claw.status)
  const now = useNow(running)
  const seconds = elapsedSeconds(claw, now)
  const text = formatElapsed(seconds)

  // Tabular figures stop the value jittering as digits change width, which is
  // distracting on something that updates every second.
  return (
    <span
      className={cn("tabular-nums", className)}
      title={running ? "Time since this run started" : "How long this run took"}
    >
      {text}
    </span>
  )
}
