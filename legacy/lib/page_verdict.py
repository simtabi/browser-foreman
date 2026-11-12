"""Decide what a fetched page really is, from its title and opening text.

Shared by content_check.py (plain HTTP) and the renderer's records (headless Chrome), so both use one set of rules.
Strong signals give a verdict; weak ones only raise a flag for the owner to judge.
"""
import re

# The whole title is an error message: a soft 404.
SOFT404_TITLE = re.compile(
    r"^\s*(error\s*)?(404|410)\b|^\s*(page|file|post|article|resource|project|repository)?\s*not\s+found\s*([-|:–—].{0,40})?$|"
    r"^\s*(oops|sorry|whoops)[!.,]?\s.*(not\s+found|can.?t\s+(be\s+)?find|doesn.?t\s+exist|no\s+longer)|"
    r"page\s+(cannot|can.?t|could\s+not)\s+be\s+found|page\s+(does\s+not|doesn.?t)\s+exist|"
    r"^\s*(this\s+)?page\s+(is\s+)?(no\s+longer\s+available|has\s+been\s+removed|unavailable)",
    re.I)
# Domain parking and for-sale pages.
PARKED = re.compile(
    r"(this\s+)?domain\s+(name\s+)?(is\s+)?(for\s+sale|may\s+be\s+for\s+sale|is\s+parked|has\s+expired)|buy\s+this\s+domain|"
    r"domain\s+parking|parked\s+(free|domain)|hugedomains|dan\.com|\bsedo\b|afternic|godaddy\s+(domain\s+)?auction|"
    r"the\s+domain\s+.{0,40}\s+is\s+for\s+sale|inquire\s+about\s+this\s+domain|domain\s+expired|"
    r"^\s*[\w-]+(\.[\w-]+)+\s+is\s+for\s+sale|make\s+(an\s+)?offer\s+on\s+this\s+domain",
    re.I)
# Hosting accounts that were shut off.
SUSPENDED = re.compile(r"account\s+(has\s+been\s+)?suspended|this\s+account\s+has\s+been\s+suspended|"
                       r"site\s+(has\s+been\s+)?(suspended|deactivated|disabled)|website\s+is\s+(no\s+longer\s+)?(suspended|offline)|"
                       r"no\s+longer\s+in\s+service|this\s+site\s+can.?t\s+be\s+reached|welcome\s+to\s+nginx|"
                       r"apache2?\s+(ubuntu\s+)?default\s+page|it\s+works!|index\s+of\s+/\s*$|future\s+home\s+of", re.I)
# Bot checks: the page exists, the check could not see it.
CHALLENGE = re.compile(r"just\s+a\s+moment|attention\s+required|checking\s+(if\s+the\s+site|your\s+browser)|"
                       r"verify(ing)?\s+you\s+are\s+(a\s+)?human|are\s+you\s+a\s+robot|access\s+denied|cf-chl|captcha|"
                       r"ddos[- ]guard|please\s+enable\s+(javascript\s+and\s+)?cookies|request\s+blocked", re.I)
# Weak: a "not found" message somewhere near the top, but the title does not say so.
WEAK404 = re.compile(r"\b404\b.{0,60}(not\s+found|error)|page\s+not\s+found|we\s+couldn.?t\s+find\s+(that|this|the)\s+page|"
                     r"nothing\s+(was\s+)?found\s+(here|at\s+this)|no\s+longer\s+available|has\s+been\s+(deleted|removed)",
                     re.I)


def verdict(title, text, url=""):
    """Return (verdict, signal). verdict: ok | soft-404 | parked | suspended | challenge | suspect-404."""
    title = (title or "").strip()
    head = (text or "")[:1500]
    if CHALLENGE.search(title) or (len(head) < 400 and CHALLENGE.search(head)):
        return "challenge", "bot check page: " + (title or head[:60])
    # Parking pages are short; a long article that mentions domains is not one.
    if PARKED.search(title) or (len(head) < 700 and PARKED.search(head)):
        return "parked", "parked or for-sale domain: " + (title or head[:60])
    if SUSPENDED.search(title) or (len(head) < 600 and SUSPENDED.search(head)):
        return "suspended", "site switched off: " + (title or head[:60])
    if SOFT404_TITLE.search(title) and "404" not in url:
        return "soft-404", "page says it is not found: " + title[:80]
    if WEAK404.search(head[:600]) and "404" not in url:
        return "suspect-404", "may be a 'not found' page: " + re.sub(r"\s+", " ", head[:90])
    return "ok", ""
