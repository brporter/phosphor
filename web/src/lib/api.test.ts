import { fetchSessions } from './api';

let mockFetch: ReturnType<typeof vi.fn>;

beforeEach(() => {
  mockFetch = vi.fn();
  vi.stubGlobal('fetch', mockFetch);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('fetchSessions', () => {
  it('fetchSessions_Success — returns parsed session array on ok response', async () => {
    const sessions = [
      { id: 'abc123', mode: 'pty', cols: 80, rows: 24, command: 'bash', viewers: 2 },
      { id: 'def456', mode: 'pipe', cols: 120, rows: 40, command: 'zsh', viewers: 0 },
    ];

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(sessions),
    });

    const result = await fetchSessions(null);

    expect(result).toEqual(sessions);
    expect(mockFetch).toHaveBeenCalledOnce();
    expect(mockFetch).toHaveBeenCalledWith('/api/sessions', {
      headers: { 'Content-Type': 'application/json' },
    });
  });

  it('fetchSessions_WithToken — sets Authorization header when token is provided', async () => {
    const sessions = [
      { id: 'xyz789', mode: 'pty', cols: 80, rows: 24, command: 'fish', viewers: 1 },
    ];

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(sessions),
    });

    const token = 'my-secret-token';
    await fetchSessions(token);

    expect(mockFetch).toHaveBeenCalledOnce();
    expect(mockFetch).toHaveBeenCalledWith('/api/sessions', {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
      },
    });
  });

  it('fetchSessions_Error — throws with status code on non-ok response', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 500,
    });

    await expect(fetchSessions(null)).rejects.toThrow('Failed to fetch sessions: 500');
    expect(mockFetch).toHaveBeenCalledOnce();
  });
});
