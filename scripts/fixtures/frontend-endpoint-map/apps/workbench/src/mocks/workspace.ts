// DEV mock store. Imported only through the import.meta.env.DEV branch, so a
// production bundle drops it; the ledger lists this file under store_files.
export async function mockWorkspaceList(limit: number): Promise<string[]> {
  return Array.from({ length: limit }, (_, index) => `mock-file-${index}.ts`);
}
