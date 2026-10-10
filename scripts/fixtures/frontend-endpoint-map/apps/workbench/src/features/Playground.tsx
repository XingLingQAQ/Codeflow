// Two shapes:
//   - openPlayground: a URL expression this extractor cannot resolve (the host
//     carries the value wherever the build points it). It must be listed as
//     unknown - never guessed into a route.
//   - listPlaygroundSessions: a plain path neither the contract nor the router
//     knows -> status orphan_client (the call site is the only evidence).
import { get } from '../../api';

export async function openPlayground(sessionToken: string): Promise<void> {
  const host = new URL(window.location.href).origin;
  await get(`${host}/api/v1/playground/${sessionToken}`);
}

export async function listPlaygroundSessions(): Promise<unknown> {
  return get('/api/v1/playground/sessions');
}
