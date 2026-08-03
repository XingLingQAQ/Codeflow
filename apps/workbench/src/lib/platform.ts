/** Platform detection for keyboard hints and modifier handling. */

const nav = typeof navigator !== 'undefined' ? navigator : undefined;

export const isMac: boolean = /mac|iphone|ipad|ipod/i.test(
  // userAgentData.platform is the modern source; fall back to legacy fields.
  ((nav as { userAgentData?: { platform?: string } } | undefined)?.userAgentData?.platform ??
    nav?.platform ??
    nav?.userAgent ??
    ''),
);

/** Human label for the primary command modifier: ⌘ on macOS, Ctrl elsewhere. */
export function modLabel(): string {
  return isMac ? '⌘' : 'Ctrl';
}

/** True when the platform command modifier is held (⌘ on macOS, Ctrl elsewhere). */
export function isModEvent(e: { metaKey: boolean; ctrlKey: boolean }): boolean {
  return isMac ? e.metaKey : e.ctrlKey;
}
