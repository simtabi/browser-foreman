"""One small validator for every request body the server accepts: rules are built by chaining, checked in one place,
and a failure names the field and the reason. Nothing here trusts the page; the page validates too, for messages.

    CLOSE = V.obj(items=V.list(V.obj(browser=V.enum(*CLOSABLE), window_id=V.int().optional(),
                                      tab_id=V.int().optional(), pos=V.int(0, 100000), url=V.url()), max_items=1000))
    data = CLOSE.check(body)            # returns the cleaned value or raises Invalid("items[3].url: not a web address")
"""
import re


class Invalid(ValueError):
    pass


class Rule:
    """A chainable rule. Each method returns the rule, so `V.str().max(40).optional()` reads as it checks."""

    def __init__(self, kind, check):
        self.kind, self._check, self._optional, self._default, self._extra = kind, check, False, None, []

    def optional(self, default=None):
        self._optional, self._default = True, default
        return self

    def where(self, test, message):
        """An extra condition on the checked value."""
        self._extra.append((test, message))
        return self

    def check(self, value, path="body"):
        if value is None:
            if self._optional:
                return self._default
            raise Invalid(f"{path}: required")
        v = self._check(value, path)
        for test, message in self._extra:
            if not test(v):
                raise Invalid(f"{path}: {message}")
        return v


class V:
    """Rule makers."""

    @staticmethod
    def str(min_len=0, max_len=10_000):
        def c(v, p):
            if not isinstance(v, str):
                raise Invalid(f"{p}: must be text")
            if not (min_len <= len(v) <= max_len):
                raise Invalid(f"{p}: length must be {min_len}–{max_len}")
            return v
        return Rule("str", c)

    @staticmethod
    def int(lo=-(2 ** 53), hi=2 ** 53):
        def c(v, p):
            if isinstance(v, bool) or not isinstance(v, int):
                raise Invalid(f"{p}: must be a whole number")
            if not (lo <= v <= hi):
                raise Invalid(f"{p}: must be between {lo} and {hi}")
            return v
        return Rule("int", c)

    @staticmethod
    def bool():
        def c(v, p):
            if not isinstance(v, bool):
                raise Invalid(f"{p}: must be true or false")
            return v
        return Rule("bool", c)

    @staticmethod
    def enum(*values):
        def c(v, p):
            if v not in values:
                raise Invalid(f"{p}: must be one of {', '.join(map(str, values))}")
            return v
        return Rule("enum", c)

    @staticmethod
    def match(pattern, message="has the wrong format", max_len=200):
        rx = re.compile(pattern)

        def c(v, p):
            if not isinstance(v, str) or len(v) > max_len or not rx.fullmatch(v):
                raise Invalid(f"{p}: {message}")
            return v
        return Rule("match", c)

    @staticmethod
    def url(max_len=4096):
        """A web address only: http or https, no spaces, no control characters."""
        def c(v, p):
            if not isinstance(v, str) or len(v) > max_len or not re.fullmatch(r"https?://[^\s\x00-\x1f]+", v, re.I):
                raise Invalid(f"{p}: not a web address")
            return v
        return Rule("url", c)

    @staticmethod
    def list(item, max_items=1000, min_items=0):
        def c(v, p):
            if not isinstance(v, list):
                raise Invalid(f"{p}: must be a list")
            if not (min_items <= len(v) <= max_items):
                raise Invalid(f"{p}: needs {min_items}–{max_items} entries")
            return [item.check(x, f"{p}[{k}]") for k, x in enumerate(v)]
        return Rule("list", c)

    @staticmethod
    def obj(**fields):
        """An object with exactly these fields (unknown fields are refused, not ignored)."""
        def c(v, p):
            if not isinstance(v, dict):
                raise Invalid(f"{p}: must be an object")
            extra = set(v) - set(fields)
            if extra:
                raise Invalid(f"{p}: unknown field {sorted(extra)[0]}")
            return {k: rule.check(v.get(k), f"{p}.{k}") for k, rule in fields.items()}
        return Rule("obj", c)


SNAPSHOT_ID = V.match(r"\d{4}-\d{2}-\d{2}_\d{6}", "not a snapshot id")
DAY = V.match(r"\d{4}-\d{2}-\d{2}", "not a date (YYYY-MM-DD)")
RUN_ID = V.match(r"\d{4}-\d{2}-\d{2}_\d{6}-[0-9a-f]{4}", "not a re-check id")
