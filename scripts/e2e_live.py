#!/usr/bin/env python3
"""Live end-to-end checks against the running stack for the two newest features:
edit-listing (PATCH /properties/:id) and the host's public reply to a guest
review (POST /reviews/:id/response). Exits non-zero if any assertion fails.
"""
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

KC_URL = os.environ.get("KC_URL", "http://localhost:8080")
API_URL = os.environ.get("API_URL", "http://localhost:8081/api/v1")
fails = []


def token(u, p):
    data = urllib.parse.urlencode({
        "client_id": "airhost-web", "grant_type": "password",
        "username": u, "password": p, "scope": "openid",
    }).encode()
    url = f"{KC_URL}/realms/airhost/protocol/openid-connect/token"
    with urllib.request.urlopen(urllib.request.Request(url, data=data)) as r:
        return json.load(r)["access_token"]


def api(method, path, tok=None, body=None):
    headers = {}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if tok:
        headers["Authorization"] = f"Bearer {tok}"
    req = urllib.request.Request(f"{API_URL}{path}", data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req) as r:
            txt = r.read().decode()
            return r.status, (json.loads(txt) if txt else None)
    except urllib.error.HTTPError as e:
        txt = e.read().decode()
        try:
            return e.code, json.loads(txt)
        except Exception:
            return e.code, txt


def check(name, cond, detail=""):
    print(("  PASS " if cond else "  FAIL ") + name + (f" — {detail}" if detail and not cond else ""))
    if not cond:
        fails.append(name)


def listmine(tok):
    _, body = api("GET", "/host/properties", tok)
    return body.get("items", body) if isinstance(body, dict) else body


def main():
    host = token("host@airhost.dev", "host123")

    print("\n[1] Edit listing (PATCH /properties/:id)")
    props = listmine(host)
    target = next((p for p in props if "Porto" in p["title"] or "Clérigos" in p["title"]), props[0])
    pid = target["id"]
    _, before = api("GET", f"/properties/{pid}", host)
    new_title = "Design flat near Clérigos — refreshed for 2026"
    new_price = before["pricePerNight"]["amountCents"] + 1500
    st, after = api("PATCH", f"/properties/{pid}", host, {
        "title": new_title,
        "description": before.get("description", ""),
        "priceCents": new_price,
        "cleaningFeeCents": before["cleaningFee"]["amountCents"],
        "currency": before["pricePerNight"]["currency"],
        "maxGuests": before["maxGuests"],
        "cancellationPolicy": before["cancellationPolicy"],
        "weeklyDiscountPct": before["weeklyDiscountPct"],
        "monthlyDiscountPct": before["monthlyDiscountPct"],
        "taxRatePct": before["taxRatePct"],
        "instantBook": True,  # regression: editing must NOT silently reset this
    })
    check("PATCH returns 200", st == 200, f"status={st} body={after}")
    _, reloaded = api("GET", f"/properties/{pid}", host)
    check("title updated", reloaded["title"] == new_title, reloaded.get("title"))
    check("price updated", reloaded["pricePerNight"]["amountCents"] == new_price, reloaded["pricePerNight"]["amountCents"])
    check("instantBook honored (not reset)", reloaded["instantBook"] is True, reloaded.get("instantBook"))

    print("\n[2] Host responds to a guest review (POST /reviews/:id/response)")
    # Find a property of the host that has reviews without a response.
    review = None
    rprop = None
    for p in props:
        _, revs = api("GET", f"/properties/{p['id']}/reviews", host)
        items = revs.get("items", []) if isinstance(revs, dict) else []
        cand = next((r for r in items if not r.get("response")), None)
        if cand:
            review, rprop = cand, p["id"]
            break
    check("found a review awaiting a response", review is not None)
    if review:
        rid = review["id"]
        reply = "Thank you so much for the kind words — it was a pleasure hosting you! "
        st, resp = api("POST", f"/reviews/{rid}/response", host, {"response": reply.strip()})
        check("response POST returns 200", st == 200, f"status={st} body={resp}")
        if st == 200:
            check("response text saved", resp.get("response") == reply.strip(), resp.get("response"))
            check("respondedAt set", bool(resp.get("respondedAt")), resp.get("respondedAt"))
        # Confirm it is visible on the listing's reviews.
        _, revs2 = api("GET", f"/properties/{rprop}/reviews", host)
        items2 = revs2.get("items", []) if isinstance(revs2, dict) else []
        match = next((r for r in items2 if r["id"] == rid), None)
        check("response visible on listing", bool(match and match.get("response")), match)

    print("\n" + ("ALL LIVE E2E CHECKS PASSED" if not fails else f"FAILURES: {fails}"))
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
