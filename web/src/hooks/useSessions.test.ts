import { renderHook, waitFor, act } from "@testing-library/react";
import { useSessions } from "./useSessions";

vi.mock("../lib/api");
vi.mock("../auth/useAuth");

import { fetchSessions } from "../lib/api";
import { useAuth } from "../auth/useAuth";

const mockFetchSessions = vi.mocked(fetchSessions);
const mockUseAuth = vi.mocked(useAuth);

beforeEach(() => {
  mockUseAuth.mockReturnValue({
    user: null,
    isLoading: false,
    login: vi.fn(),
    logout: vi.fn(),
    getToken: vi.fn(() => "test-token"),
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useSessions", () => {
  it("fetches sessions on mount and sets isLoading to false", async () => {
    const mockSessions = [
      { id: "s1", mode: "pty", cols: 80, rows: 24, command: "bash", viewers: 0 },
      { id: "s2", mode: "pipe", cols: 120, rows: 40, command: "zsh", viewers: 1 },
    ];

    mockFetchSessions.mockResolvedValue(mockSessions as never);

    const { result } = renderHook(() => useSessions());

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.sessions).toHaveLength(2);
    expect(result.current.error).toBeNull();
  });

  it("sets error when fetch fails", async () => {
    mockFetchSessions.mockRejectedValue(new Error("Network error"));

    const { result } = renderHook(() => useSessions());

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.error).toBe("Network error");
    expect(result.current.sessions).toHaveLength(0);
  });

  it("polls at the specified interval", async () => {
    const mockSessions = [
      { id: "s1", mode: "pty", cols: 80, rows: 24, command: "bash", viewers: 0 },
    ];

    mockFetchSessions.mockResolvedValue(mockSessions as never);

    // Use a very short real interval to avoid fake timer + async interaction issues
    const { result } = renderHook(() => useSessions(100));

    // Wait for initial fetch
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    const initialCalls = mockFetchSessions.mock.calls.length;

    // Wait for at least one poll cycle
    await waitFor(() =>
      expect(mockFetchSessions.mock.calls.length).toBeGreaterThan(initialCalls),
    );
  });
});
