import { fetchAuthConfig, generateApiKey } from './api';

let mockFetch: ReturnType<typeof vi.fn>;

beforeEach(() => {
  mockFetch = vi.fn();
  vi.stubGlobal('fetch', mockFetch);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('fetchAuthConfig', () => {
  it('returns the provider list on ok response', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ providers: ['google', 'microsoft'] }),
    });

    const result = await fetchAuthConfig();

    expect(result).toEqual({ providers: ['google', 'microsoft'] });
    expect(mockFetch).toHaveBeenCalledWith('/api/auth/config');
  });

  it('returns an empty provider list on non-ok response', async () => {
    mockFetch.mockResolvedValueOnce({ ok: false, status: 500 });

    const result = await fetchAuthConfig();

    expect(result).toEqual({ providers: [] });
  });
});

describe('generateApiKey', () => {
  it('sets the Authorization header and returns the key', async () => {
    const key = { api_key: 'phk:abc', key_id: 'k1' };
    mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(key) });

    const result = await generateApiKey('my-token');

    expect(result).toEqual(key);
    expect(mockFetch).toHaveBeenCalledWith('/api/auth/api-key', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer my-token',
      },
    });
  });

  it('throws with the status code on non-ok response', async () => {
    mockFetch.mockResolvedValueOnce({ ok: false, status: 403 });

    await expect(generateApiKey(null)).rejects.toThrow('Failed to generate API key: 403');
  });
});
