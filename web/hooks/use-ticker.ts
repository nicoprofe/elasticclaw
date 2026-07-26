"use client"

import { useEffect, useState } from "react"

/**
 * useNow returns the current time, re-rendering on an interval so a duration
 * derived from it counts up on its own.
 *
 * The elapsed time on a claw card used to change only when hub data arrived, which
 * made a running task look stalled between updates. This drives it from a clock
 * instead.
 *
 * Pass enabled=false for a run that has finished: its duration is fixed, so
 * re-rendering every second would be wasted work on a dashboard that may be showing
 * a long list of them.
 */
export function useNow(enabled: boolean, intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!enabled) return
    // Set immediately as well as on the interval: a card that mounts just after a
    // tick would otherwise show a value up to a second stale, which is visible when
    // several cards are on screen and their timers disagree.
    setNow(Date.now())
    const id = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(id)
  }, [enabled, intervalMs])

  return now
}
