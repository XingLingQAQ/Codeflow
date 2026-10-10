// Stands in for @tanstack/react-query so the fixture needs no dependency.
export function useQuery<T>(options: { queryKey: unknown[]; queryFn: () => Promise<T> }) {
  void options;
  return { data: undefined as T | undefined };
}
