"""Per-site politeness for every tool that fetches bookmarks, so a run never trips a site's rate limiting.

What it does, per registrable domain (github.com and gist.github.com share one budget):
- caps how many requests run at once, and spaces their starts;
- backs off when a site says so (429 / 503, honouring Retry-After), doubling up to 15 minutes;
- trips a circuit breaker after repeated timeouts or connection failures, and cools the site down;
- while a site cools down, callers get "try later" instead of waiting, so a blocked site is never reported dead.
interleave() orders work round-robin by site, so no site gets a burst.
"""
import threading
import time
from collections import OrderedDict, defaultdict
from urllib.parse import urlsplit

# (concurrent requests, seconds between starts). Sites known to rate-limit get one request at a time.
RULES = {
    "github.com": (1, 1.2), "githubusercontent.com": (1, 1.0), "stackoverflow.com": (1, 2.0),
    "stackexchange.com": (1, 2.0), "serverfault.com": (1, 2.0), "superuser.com": (1, 2.0), "askubuntu.com": (1, 2.0),
    "medium.com": (1, 1.5), "youtube.com": (1, 1.0), "google.com": (1, 1.5), "reddit.com": (1, 2.0),
    "linkedin.com": (1, 3.0), "twitter.com": (1, 2.0), "x.com": (1, 2.0), "facebook.com": (1, 2.0),
}
DEFAULT = (2, 0.3)
MAX_WAIT = 45          # longest a caller waits for a cooling site before giving up with "try later"
STRIKES = 3            # consecutive network failures that trip the breaker
BREAKER_COOLDOWN = 300  # seconds
MAX_BACKOFF = 900


def site(url_or_host):
    host = urlsplit(url_or_host).hostname if "/" in url_or_host else url_or_host
    p = (host or "").lower().removeprefix("www.").split(".")
    n = 3 if len(p) > 2 and p[-2] in ("co", "com", "org", "ac", "gov", "net") and len(p[-1]) == 2 else 2
    return ".".join(p[-n:])


class HostLimiter:
    def __init__(self):
        self._lock = threading.Lock()
        self._sem = {}
        self._next = defaultdict(float)      # earliest start time for the next request
        self._cool = defaultdict(float)      # cooling down until
        self._backoff = defaultdict(float)
        self._strikes = defaultdict(int)
        self.events = []                     # (time, site, what) for reporting

    def _rule(self, s):
        return RULES.get(s, DEFAULT)

    def acquire(self, url):
        """Wait for a slot. Returns the site key, or None when the site is cooling down for longer than MAX_WAIT."""
        s = site(url)
        with self._lock:
            sem = self._sem.setdefault(s, threading.Semaphore(self._rule(s)[0]))
        deadline = time.time() + MAX_WAIT
        if not sem.acquire(timeout=MAX_WAIT):
            return None
        while True:
            with self._lock:
                now = time.time()
                start = max(self._next[s], self._cool[s])
                if start <= now:
                    self._next[s] = now + self._rule(s)[1]
                    return s
            if start > deadline:
                sem.release()
                return None
            time.sleep(min(start - time.time(), 5) if start > time.time() else 0)

    def release(self, s, outcome, retry_after=None):
        """outcome: 'ok' | 'ratelimit' | 'neterr' | 'other'."""
        with self._lock:
            now = time.time()
            if outcome == "ratelimit":
                b = min(max(self._backoff[s] * 2, 30), MAX_BACKOFF)
                if retry_after:
                    b = min(max(b, retry_after), MAX_BACKOFF)
                self._backoff[s] = b
                self._cool[s] = now + b
                self.events.append((now, s, f"rate limited, pausing {int(b)}s"))
            elif outcome == "neterr":
                self._strikes[s] += 1
                if self._strikes[s] >= STRIKES:
                    self._cool[s] = now + BREAKER_COOLDOWN
                    self._strikes[s] = 0
                    self.events.append((now, s, f"{STRIKES} network failures in a row, pausing {BREAKER_COOLDOWN}s"))
            else:
                self._strikes[s] = 0
                if outcome == "ok":
                    self._backoff[s] = 0
        self._sem[s].release()

    def cooling(self, url):
        return self._cool[site(url)] - time.time()


def interleave(urls):
    """Round-robin by site, biggest sites first, so consecutive requests rarely hit the same site."""
    buckets = OrderedDict()
    for u in urls:
        buckets.setdefault(site(u), []).append(u)
    queues = sorted(buckets.values(), key=len, reverse=True)
    out = []
    while queues:
        for q in queues:
            out.append(q.pop(0))
        queues = [q for q in queues if q]
    return out


LIMITER = HostLimiter()
