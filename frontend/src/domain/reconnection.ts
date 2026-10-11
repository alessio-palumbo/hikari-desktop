export const RECONNECTION_GRACE_MS = 3000;

// This only controls presentation. Discovery retries must not wait for it.
export function reconnectionStatus(error: string | undefined, startedAt: number | undefined, now: number): { message: string; showActions: boolean } | undefined {
  if (!error) return undefined;
  if (startedAt !== undefined && now - startedAt < RECONNECTION_GRACE_MS) {
    return { message: 'reconnecting LAN devices', showActions: false };
  }
  return { message: error, showActions: true };
}
