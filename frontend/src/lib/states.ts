import type { EventLevel, GameState } from "./types.ts";

// How each game state and event level looks.
export const STATES: Record<GameState, { label: string }> = {
  idling: { label: "Idling" },
  starting: { label: "Starting" },
  waiting: { label: "Waiting" },
  queued: { label: "Queued" },
  failing: { label: "Problem" },
  done: { label: "Done" },
  refundable: { label: "Refundable" },
  stopped: { label: "Stopped" },
};

export const LEVEL_DOT: Record<EventLevel, string> = { good: "bg-good", warn: "bg-warn", bad: "bg-bad", info: "bg-faint" };

export function stateOf(name: GameState): { label: string } {
  return STATES[name] ?? STATES.stopped;
}
