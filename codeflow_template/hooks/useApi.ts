import { useState, useEffect, useRef, useCallback } from 'react';

interface UseApiState<T> {
  data: T | null;
  loading: boolean;
  error: Error | null;
}

interface UseApiResult<T> extends UseApiState<T> {
  refetch: () => void;
  mutate: (data: T | null) => void;
}

/**
 * Generic data-fetching hook with loading/error/data states.
 * Automatically cancels in-flight requests on unmount or deps change.
 *
 * @param fetcher - async function that receives an AbortSignal and returns data
 * @param deps - dependency array; refetches when any dep changes
 * @param options.enabled - if false, skip fetching (default: true)
 */
export function useApi<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  deps: unknown[] = [],
  options?: { enabled?: boolean },
): UseApiResult<T> {
  const [state, setState] = useState<UseApiState<T>>({
    data: null,
    loading: true,
    error: null,
  });

  const mountedRef = useRef(true);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  const enabled = options?.enabled !== false;

  const doFetch = useCallback(() => {
    if (!enabled) {
      setState(prev => ({ ...prev, loading: false }));
      return () => {};
    }

    const controller = new AbortController();
    setState(prev => ({ ...prev, loading: true, error: null }));

    fetcherRef.current(controller.signal)
      .then(data => {
        if (mountedRef.current && !controller.signal.aborted) {
          setState({ data, loading: false, error: null });
        }
      })
      .catch(err => {
        if (mountedRef.current && !controller.signal.aborted) {
          setState(prev => ({ ...prev, loading: false, error: err }));
        }
      });

    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, ...deps]);

  useEffect(() => {
    const cleanup = doFetch();
    return cleanup;
  }, [doFetch]);

  useEffect(() => {
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const refetch = useCallback(() => {
    doFetch();
  }, [doFetch]);

  const mutate = useCallback((data: T | null) => {
    setState(prev => ({ ...prev, data }));
  }, []);

  return { ...state, refetch, mutate };
}

/**
 * Hook for mutation operations (POST/PUT/PATCH/DELETE).
 * Returns an execute function + loading/error state.
 */
export function useMutation<TInput, TOutput>(
  mutationFn: (input: TInput, signal: AbortSignal) => Promise<TOutput>,
): {
  execute: (input: TInput) => Promise<TOutput>;
  loading: boolean;
  error: Error | null;
} {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const execute = useCallback(
    async (input: TInput): Promise<TOutput> => {
      const controller = new AbortController();
      setLoading(true);
      setError(null);
      try {
        const result = await mutationFn(input, controller.signal);
        if (mountedRef.current) setLoading(false);
        return result;
      } catch (err) {
        if (mountedRef.current) {
          setLoading(false);
          setError(err instanceof Error ? err : new Error(String(err)));
        }
        throw err;
      }
    },
    [mutationFn],
  );

  return { execute, loading, error };
}
