"""Settings from the environment, with .env support: one place reads them, everything else asks here.

The project root's .env is read once, at import, into os.environ. A variable already set in the real environment
wins, so `BM_PORT=9000 python3 src/server/serve.py` overrides the file. Format: KEY=VALUE per line, # comments,
optional single or double quotes, `export ` prefix allowed. Nothing in .env is ever sent anywhere.

    from config import env
    port = env.int("BM_PORT", 8765)
    autosave = env.bool("BM_NO_TAB_AUTOSAVE", False)
"""
import os
import re

_LINE = re.compile(r"^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$")


def load_dotenv(path):
    """Read KEY=VALUE lines into os.environ without replacing what is already set. Returns the keys it set."""
    if not os.path.isfile(path):
        return []
    set_keys = []
    with open(path, encoding="utf-8") as f:
        for raw in f:
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            m = _LINE.match(line)
            if not m:
                continue
            key, value = m.group(1), m.group(2)
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
                value = value[1:-1]
            elif " #" in value:
                value = value.split(" #", 1)[0].rstrip()
            if key not in os.environ:
                os.environ[key] = value
                set_keys.append(key)
    return set_keys


class Env:
    """Typed reads of environment variables, each with a default. Chainable defaults are not needed here: one call,
    one value, so a reader always sees the variable's name and its fallback together."""

    def str(self, name, default=""):
        v = os.environ.get(name)
        return default if v is None or v == "" else v

    def int(self, name, default=0):
        try:
            return int(self.str(name, str(default)))
        except ValueError:
            return default

    def bool(self, name, default=False):
        v = self.str(name, "").lower()
        return default if v == "" else v in ("1", "true", "yes", "on")

    def path(self, name, default):
        v = self.str(name, "")
        return os.path.realpath(os.path.expanduser(v)) if v else default


env = Env()
