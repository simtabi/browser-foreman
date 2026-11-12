"""Check every web bookmark once, without credentials. Resumable: rows already in the CSV are skipped.

Usage: python3 check_links.py work/bookmarks.json work/links.csv
"""
import csv
import json
import os
import re
import ipaddress
import socket
import ssl
import sys
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from urllib.parse import urlsplit

from hostlimit import LIMITER, interleave, site

UA = ("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/131.0 Safari/537.36")
TIMEOUT = 15
WORKERS = 24
PER_HOST = 2
LOGIN = re.compile(r"(login|log-in|signin|sign-in|sign_in|/auth|/sso|oauth|/account/|accounts\.|session/new|idp\.|cas/)", re.I)
ACTION = re.compile(r"(unsubscribe|logout|logoff|signout|confirm|activate|verify|reset|delete|remove|token=|key=|apikey|/invite)", re.I)
LOCAL = re.compile(r"(\.test$|\.local$|^localhost$|^127\.|^192\.168\.|^10\.|^stg-|^staging\.)")

host_locks = {}
hl_guard = threading.Lock()


def host_sem(host):
    with hl_guard:
        return host_locks.setdefault(host, threading.Semaphore(PER_HOST))


CTX = ssl.create_default_context()
_PRIV = {}


def private_host(host):
    """True when the name is, or resolves to, an address on this machine or the local network (or cannot be
    resolved to a public one). A bookmark, or a redirect from one, must never make the checker poke the router,
    a printer or a service on localhost."""
    host = (host or "").strip("[]").rstrip(".").lower()
    if not host or host == "localhost" or host.endswith((".localhost", ".local", ".internal", ".lan", ".home.arpa")):
        return True
    if host in _PRIV:
        return _PRIV[host]
    try:
        addrs = {ai[4][0] for ai in socket.getaddrinfo(host, None)}
    except OSError:
        _PRIV[host] = False  # unresolvable: the request will fail on its own
        return False
    bad = False
    for a in addrs:
        ip = ipaddress.ip_address(a.split("%")[0])
        if getattr(ip, "ipv4_mapped", None):
            ip = ip.ipv4_mapped
        if ip.is_private or ip.is_loopback or ip.is_link_local or ip.is_reserved or ip.is_multicast or ip.is_unspecified:
            bad = True
    _PRIV[host] = bad
    return bad


class _SafeRedirects(urllib.request.HTTPRedirectHandler):
    """Every redirect hop is checked like the original link: no private addresses, no action links, web only."""
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        u = urlsplit(newurl)
        if u.scheme not in ("http", "https") or private_host(u.hostname) or ACTION.search(newurl):
            raise urllib.error.HTTPError(newurl, 599, "redirect to a local or action address refused", headers, fp)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


_OPENER = urllib.request.build_opener(_SafeRedirects(), urllib.request.HTTPSHandler(context=CTX))


def fetch(url, method):
    req = urllib.request.Request(url, method=method, headers={
        "User-Agent": UA, "Accept": "text/html,application/xhtml+xml,*/*;q=0.8",
        "Accept-Language": "en-US,en;q=0.9"})
    with _OPENER.open(req, timeout=TIMEOUT) as r:
        if method == "GET":
            r.read(2048)
        return r.status, r.geturl()


def check(url):
    host = urlsplit(url).hostname or ""
    if LOCAL.search(host) or private_host(host):
        return "local", "", url, "local or staging host"
    if ACTION.search(url):
        return "skipped", "", url, "looks like an action link; not visited"
    last = None
    for method in ("HEAD", "GET"):
        key = LIMITER.acquire(url)
        if key is None:
            return "unverified", "", url, f"{site(url)} is cooling down after rate limiting; try later"
        try:
            code, final = fetch(url, method)
            LIMITER.release(key, "ok")
            return classify(url, code, final)
        except urllib.error.HTTPError as e:
            last = e
            if e.code in (429, 503):
                ra = e.headers.get("Retry-After")
                LIMITER.release(key, "ratelimit", int(ra) if ra and ra.isdigit() else None)
                return "unverified", e.code, url, f"HTTP {e.code}: the site asked us to slow down; try later"
            LIMITER.release(key, "other")
            if method == "HEAD" and e.code in (400, 403, 404, 405, 406, 500, 501):
                continue
            return classify_err(url, e)
        except Exception as e:  # noqa: BLE001
            last = e
            r = getattr(e, "reason", e)
            net = isinstance(r, (TimeoutError, socket.timeout, ConnectionResetError, ConnectionRefusedError, OSError)) \
                and not isinstance(r, socket.gaierror)
            LIMITER.release(key, "neterr" if net else "other")
            if method == "HEAD":
                continue
            return classify_exc(url, e)
    return classify_exc(url, last)


def classify(url, code, final):
    if LOGIN.search(urlsplit(final).path + urlsplit(final).netloc) and not LOGIN.search(url):
        return "needs-login", code, final, "redirects to a sign-in page"
    a, b = urlsplit(url), urlsplit(final)
    same = (a.netloc.lower().removeprefix("www.") == b.netloc.lower().removeprefix("www.")
            and a.path.rstrip("/") == b.path.rstrip("/"))
    if same:
        return "ok", code, final, ""
    if a.path.strip("/") and not b.path.strip("/"):
        return "unverified", code, final, "redirects to the home page; page may be gone"
    return "moved", code, final, "redirects"


def classify_err(url, e):
    c = e.code
    if c in (404, 410):
        return "dead", c, url, f"HTTP {c}"
    if c == 401:
        return "needs-login", c, url, "HTTP 401"
    return "unverified", c, url, f"HTTP {c}"


def classify_exc(url, e):
    r = getattr(e, "reason", e)
    s = f"{type(r).__name__}: {r}"
    if isinstance(r, socket.gaierror):
        return "dead", "", url, "domain does not resolve: " + s
    if isinstance(r, ConnectionRefusedError):
        return "dead", "", url, "connection refused"
    if isinstance(r, ssl.SSLError) or "CERTIFICATE" in s.upper():
        return "unverified", "", url, "TLS problem: " + s[:120]
    return "unverified", "", url, s[:160]


def main(src, out):
    items = json.load(open(src))["items"]
    urls = sorted({i["url"] for i in items if i["scheme"] in ("http", "https")})
    done = set()
    if os.path.exists(out):
        with open(out, newline="") as f:
            done = {row["url"] for row in csv.DictReader(f)}
    todo = interleave([u for u in urls if u not in done])
    print(f"{len(urls)} unique web links, {len(done)} already checked, {len(todo)} to go", flush=True)
    new = not os.path.exists(out)
    f = open(out, "a", newline="")
    w = csv.writer(f)
    if new:
        w.writerow(["url", "status", "code", "final_url", "note"])
    socket.setdefaulttimeout(TIMEOUT)
    start, n = time.time(), 0
    with ThreadPoolExecutor(WORKERS) as ex:
        futs = {ex.submit(check, u): u for u in todo}
        for fu in as_completed(futs):
            u = futs[fu]
            try:
                st, code, final, note = fu.result()
            except Exception as e:  # noqa: BLE001
                st, code, final, note = "unverified", "", u, f"checker error: {e}"[:160]
            w.writerow([u, st, code, final, note])
            n += 1
            if n % 200 == 0:
                f.flush()
                print(f"{n}/{len(todo)}  {time.time() - start:.0f}s", flush=True)
    f.close()
    print("done", flush=True)


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
