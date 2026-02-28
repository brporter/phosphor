import { useCallback, useEffect, useState } from "react";
import { fetchSessions } from "../lib/api";
import { useAuth } from "../auth/useAuth";
import type { SessionData } from "../components/SessionCard";

export function useSessions(pollIntervalMs = 5000) {
  const { getToken } = useAuth();
  const [sessions, setSessions] = useState<SessionData[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const data = await fetchSessions(getToken());
      setSessions(data);
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

  return { sessions, isLoading, error, refresh };
}
