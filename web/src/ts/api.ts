// One client for the local server. Reads are plain GETs; every write carries the run's token (the page's tokenPost).
// Chainable: api.path("tabs", "timeline").query({days: 7}).get<Timeline>()

export class ApiError extends Error {
  constructor(message: string, readonly status: number) { super(message); }
}

class Request {
  private parts: string[] = [];
  private params = new URLSearchParams();

  constructor(base: string[]) { this.parts = [...base]; }

  path(...segments: (string | number)[]): Request {
    for (const s of segments) this.parts.push(encodeURIComponent(String(s)));
    return this;
  }

  query(q: Record<string, string | number | boolean | undefined>): Request {
    for (const [k, v] of Object.entries(q)) if (v !== undefined) this.params.set(k, String(v));
    return this;
  }

  private get url(): string {
    const qs = this.params.toString();
    return "api/" + this.parts.join("/") + (qs ? "?" + qs : "");
  }

  async get<T>(): Promise<T> {
    const r = await fetch(this.url, { cache: "no-store" });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new ApiError((j as { error?: string }).error ?? `server answered ${r.status}`, r.status);
    return j as T;
  }

  post<T>(body: unknown = {}): Promise<T> {
    return window.tokenPost<T>(this.url, body);
  }
}

export const api = {
  path: (...segments: (string | number)[]): Request => new Request([]).path(...segments),
};
