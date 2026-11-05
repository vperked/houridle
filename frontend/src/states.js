// How each game state and event level looks.
export const STATES = {
  idling: { label: "Idling" },
  starting: { label: "Starting" },
  waiting: { label: "Waiting" },
  queued: { label: "Queued" },
  failing: { label: "Problem" },
  done: { label: "Done" },
  stopped: { label: "Stopped" },
};

export const LEVEL_DOT = { good: "bg-good", warn: "bg-warn", bad: "bg-bad", info: "bg-faint" };

export function stateOf(name) {
  return STATES[name] ?? STATES.stopped;
}
