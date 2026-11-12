"""Every folder and file Browser Foreman reads or writes, in one place. Nothing else spells out a path.

    browser-foreman/
      .env, .env.example      settings (see config.py)
      public/                 what a web server serves: index.html (the built page), .htaccess
      assets/                 css, js (the bundle), fonts, favicons
      storage/                everything the tool keeps: thumbs, sessions (tab database), workspaces, work (pipeline
                              intermediates), reports (csv, report.md), exports (import files),
                              backups (made by Settings › Backup), archive (moved-aside old files)
      src/                    lib, server, mcp, pipeline, web (TypeScript + page template), tests

Each location can be moved with an environment variable (or the .env file); the names are the ones the tools and
tests already use, so existing setups keep working.
"""
import os
import sys

from config import env, load_dotenv

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
load_dotenv(os.path.join(ROOT, ".env"))

SRC = os.path.join(ROOT, "src")
for _d in ("mcp", "pipeline", "server", "lib"):
    _p = os.path.join(SRC, _d)
    if _p not in sys.path:
        sys.path.insert(0, _p)
PUBLIC = env.path("BM_PUBLIC", os.path.join(ROOT, "public"))
ASSETS = env.path("BM_ASSETS", os.path.join(ROOT, "assets"))
STORAGE = env.path("BM_STORAGE", os.path.join(ROOT, "storage"))

THUMBS = env.path("BM_THUMBS", os.path.join(STORAGE, "thumbs"))
FAVICONS = os.path.join(ASSETS, "favicons")
SESSIONS = env.path("BM_SESSIONS", os.path.join(STORAGE, "sessions"))
WORKSPACES = env.path("BM_WORKSPACES", os.path.join(STORAGE, "workspaces"))
WORK = env.path("BM_WORK", os.path.join(STORAGE, "work"))
REPORTS = os.path.join(STORAGE, "reports")
EXPORTS = os.path.join(STORAGE, "exports")

PAGE = env.path("BM_PREVIEW", os.path.join(PUBLIC, "index.html"))  # the built page the server serves
TEMPLATE = os.path.join(SRC, "web", "template.html")
TABS_DB = env.path("BM_TABS_DB", os.path.join(SESSIONS, "tabs.sqlite3"))

# URL path → folder, for the local server. Only these are ever served.
SERVED = {
    "/assets/": ASSETS,
    "/thumbs/": THUMBS,
    "/favicons/": FAVICONS,
}
