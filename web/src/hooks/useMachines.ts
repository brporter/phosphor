import { useCallback, useEffect, useState } from "react";
import { fetchMachines, type Machine } from "../lib/machines";
import { useAuth } from "../auth/useAuth";

export function useMachines(pollIntervalMs = 5000) {
  const { getToken } = useAuth();
  const [machines, setMachines] = useState<Machine[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const data = await fetchMachines(getToken());
      setMachines(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
    } finally {
      setIsLoading(false);
    }
  }, [getToken]);

  useEffect(() => {
    void refresh();
    const interval = setInterval(() => void refresh(), pollIntervalMs);
    return () => clearInterval(interval);
  }, [refresh, pollIntervalMs]);

  return { machines, isLoading, error, refresh };
}
