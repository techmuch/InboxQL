/**
 * The release this build is: year.month.day.n, read from the repository's
 * VERSION file when the bundle is built, as the binary embeds the same file.
 */
export const appVersion: string = __APP_VERSION__;

/** The address of a release's notes: 2026.10.30.2 is #v2026-10-30-2. */
export const notesAnchor = (version: string) => `v${version.replace(/\./g, '-')}`;
