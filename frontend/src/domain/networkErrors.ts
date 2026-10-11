function errorText(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return 'device command failed';
}

export function isRecoverableNetworkError(error: unknown): boolean {
  const lower = errorText(error).toLowerCase();
  return (
    lower.includes('lifx transport has not been started') ||
    // RefreshDiscovery can report a missing named broadcast interface even
    // in Automatic mode, since the client retains its chosen runtime interface.
    lower.includes('broadcast interface') ||
    lower.includes('network interface') ||
    lower.includes('network connection') ||
    lower.includes('connection lost') ||
    lower.includes('closed network connection') ||
    lower.includes('failed to send message to device') ||
    lower.includes('send message to device')
  );
}

export function networkErrorMessage(error: unknown): string {
  const message = errorText(error);
  const lower = message.toLowerCase();
  if (lower.includes('closed network connection')) return 'Connection lost. Reconnecting discovery.';
  if (lower.includes('network interface') && (lower.includes('not available') || lower.includes('unavailable'))) {
    return 'Selected network interface unavailable. Choose another interface or Automatic.';
  }
  if (lower.includes('broadcast interface') && lower.includes('not found') && !lower.includes('no suitable broadcast interface')) {
    return 'Network interface unavailable. Refresh discovery to reconnect.';
  }
  if (lower.includes('no suitable broadcast interface') || lower.includes('broadcast interface') || lower.includes('network interfaces')) {
    return 'No network interfaces found.';
  }
  if (
    lower.includes('lifx transport has not been started') ||
    lower.includes('network connection') ||
    lower.includes('connection lost') ||
    lower.includes('failed to send message to device') ||
    lower.includes('send message to device')
  ) {
    return 'Connection lost. Refresh discovery to reconnect.';
  }
  return message;
}
