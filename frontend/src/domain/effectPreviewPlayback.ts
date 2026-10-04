type Schedule = (callback: () => void, delayMs: number) => () => void;

const scheduleTimeout: Schedule = (callback, delayMs) => {
  const timer = setTimeout(callback, delayMs);
  return () => clearTimeout(timer);
};

// Keep the final frame visible for one step before marking the bounded clip finished.
export function playEffectPreviewFrames(
  frameCount: number,
  stepMs: number,
  draw: (index: number) => void,
  finish: () => void,
  schedule: Schedule = scheduleTimeout,
): () => void {
  let disposed = false;
  let cancel: (() => void) | undefined;
  let index = 0;
  const advance = () => {
    if (disposed) return;
    if (index >= frameCount) { finish(); return; }
    draw(index++);
    cancel = schedule(advance, stepMs);
  };
  advance();
  return () => { disposed = true; cancel?.(); };
}
