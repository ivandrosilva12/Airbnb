#!/usr/bin/env python3
"""Seed the AirHost stack with realistic demo data via the real API + Keycloak.

Run against a running `docker compose up` stack (see docker-compose.yml). It logs
in as the realm's demo users (host/guest/admin), then creates listings (with a
photo, published), promo codes, wishlist favourites + a named collection, a
host<->guest conversation, and a couple of live bookings. Historical completed
stays + multi-criteria reviews are layered on by scripts/seed_history.sql.

Stdlib only — no third-party packages. Idempotency is best-effort: re-running
may create duplicate listings, but coupons/collections are name/code unique.

Env overrides: KC_URL (default http://localhost:8080),
API_URL (default http://localhost:8081/api/v1).
"""
import base64
import json
import os
import urllib.error
import urllib.parse
import urllib.request

KC_URL = os.environ.get("KC_URL", "http://localhost:8080")
API_URL = os.environ.get("API_URL", "http://localhost:8081/api/v1")
REALM = "airhost"
CLIENT_ID = "airhost-web"

USERS = {
    "host": ("host@airhost.dev", "host123"),
    "guest": ("guest@airhost.dev", "guest123"),
    "admin": ("admin@airhost.dev", "admin123"),
}

# A 1x1 PNG — enough to satisfy the "needs a photo before publishing" rule and
# the server-side image content sniff.
PNG_1PX = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
)


def token(username, password):
    data = urllib.parse.urlencode({
        "client_id": CLIENT_ID,
        "grant_type": "password",
        "username": username,
        "password": password,
        "scope": "openid",
    }).encode()
    url = f"{KC_URL}/realms/{REALM}/protocol/openid-connect/token"
    with urllib.request.urlopen(urllib.request.Request(url, data=data)) as r:
        return json.load(r)["access_token"]


def api(method, path, tok=None, body=None):
    url = f"{API_URL}{path}"
    headers = {}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if tok:
        headers["Authorization"] = f"Bearer {tok}"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
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


def upload_photo(prop_id, tok):
    boundary = "----airhostseedboundary"
    pre = (
        f"--{boundary}\r\n"
        'Content-Disposition: form-data; name="photo"; filename="seed.png"\r\n'
        "Content-Type: image/png\r\n\r\n"
    ).encode()
    post = f"\r\n--{boundary}--\r\n".encode()
    payload = pre + PNG_1PX + post
    req = urllib.request.Request(
        f"{API_URL}/properties/{prop_id}/photos",
        data=payload,
        headers={
            "Authorization": f"Bearer {tok}",
            "Content-Type": f"multipart/form-data; boundary={boundary}",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code


LISTINGS = [
    {"title": "Sunny Alfama loft with river views", "type": "apartment", "city": "Lisbon",
     "country": "PT", "lat": 38.7139, "lng": -9.1334, "priceCents": 9500, "maxGuests": 3,
     "bedrooms": 1, "beds": 2, "bathrooms": 1, "instantBook": True, "cleaningFeeCents": 2000,
     "weeklyDiscountPct": 0.1},
    {"title": "Design flat near Clérigos Tower", "type": "apartment", "city": "Porto",
     "country": "PT", "lat": 41.1456, "lng": -8.6151, "priceCents": 7800, "maxGuests": 4,
     "bedrooms": 2, "beds": 3, "bathrooms": 1, "instantBook": False, "cleaningFeeCents": 1500},
    {"title": "Gothic Quarter studio", "type": "room", "city": "Barcelona",
     "country": "ES", "lat": 41.3833, "lng": 2.1833, "priceCents": 11000, "maxGuests": 2,
     "bedrooms": 1, "beds": 1, "bathrooms": 1, "instantBook": True, "cleaningFeeCents": 2500},
    {"title": "Montmartre artist's apartment", "type": "apartment", "city": "Paris",
     "country": "FR", "lat": 48.8867, "lng": 2.3431, "priceCents": 14500, "maxGuests": 3,
     "bedrooms": 1, "beds": 2, "bathrooms": 1, "instantBook": False, "cleaningFeeCents": 3000,
     "monthlyDiscountPct": 0.15},
    {"title": "Trastevere terrace house", "type": "house", "city": "Rome",
     "country": "IT", "lat": 41.8892, "lng": 12.4694, "priceCents": 12500, "maxGuests": 5,
     "bedrooms": 2, "beds": 3, "bathrooms": 2, "instantBook": True, "cleaningFeeCents": 2800},
    {"title": "Canal-side suite in Jordaan", "type": "apartment", "city": "Amsterdam",
     "country": "NL", "lat": 52.3740, "lng": 4.8807, "priceCents": 16000, "maxGuests": 2,
     "bedrooms": 1, "beds": 1, "bathrooms": 1, "instantBook": False, "cleaningFeeCents": 3500},
]


def main():
    print(f"Seeding AirHost at {API_URL} (Keycloak {KC_URL})\n")
    tokens = {role: token(u, p) for role, (u, p) in USERS.items()}
    print("Logged in:", ", ".join(tokens))

    # Materialise each local user (the backend creates the row on first /me).
    me = {}
    for role, tok in tokens.items():
        _, body = api("GET", "/me", tok)
        me[role] = body
    print("Local users:", {r: m["id"] for r, m in me.items()})

    # --- Listings (host) ---------------------------------------------------
    amenities = ["wifi", "kitchen", "tv", "washer", "air_conditioning", "heating"]
    _, amen = api("GET", "/amenities", tokens["host"])
    if isinstance(amen, dict) and isinstance(amen.get("amenities"), list) and amen["amenities"]:
        amenities = amen["amenities"]

    created = []
    for i, l in enumerate(LISTINGS):
        body = {
            "title": l["title"], "description": f"A lovely place in {l['city']}. Booked through AirHost demo seed.",
            "type": l["type"], "addressLine1": "1 Demo Street", "city": l["city"],
            "country": l["country"], "postalCode": "0000", "latitude": l["lat"], "longitude": l["lng"],
            "priceCents": l["priceCents"], "cleaningFeeCents": l.get("cleaningFeeCents", 0), "currency": "EUR",
            "maxGuests": l["maxGuests"], "bedrooms": l["bedrooms"], "beds": l["beds"], "bathrooms": l["bathrooms"],
            "amenities": amenities[: 3 + (i % 3)], "cancellationPolicy": "moderate",
            "weeklyDiscountPct": l.get("weeklyDiscountPct", 0), "monthlyDiscountPct": l.get("monthlyDiscountPct", 0),
            "taxRatePct": 0.0, "instantBook": l["instantBook"],
        }
        st, prop = api("POST", "/properties", tokens["host"], body)
        if st not in (200, 201):
            print(f"  ! create '{l['title']}' -> {st} {prop}")
            continue
        pid = prop["id"]
        ph = upload_photo(pid, tokens["host"])
        pst, _ = api("POST", f"/properties/{pid}/publish", tokens["host"])
        created.append({"id": pid, "title": l["title"], "instant": l["instantBook"]})
        print(f"  listing {l['city']:9s} {pid}  photo={ph} publish={pst}")

    # --- Promo codes (admin) ----------------------------------------------
    for c in (
        {"code": "WELCOME10", "kind": "percentage", "percent": 0.10, "maxRedemptions": 500, "minNights": 0},
        {"code": "SUMMER25", "kind": "percentage", "percent": 0.25, "maxRedemptions": 100, "minNights": 3},
        {"code": "FLAT20EUR", "kind": "fixed", "amountCents": 2000, "currency": "EUR", "maxRedemptions": 200, "minNights": 2},
    ):
        st, r = api("POST", "/admin/coupons", tokens["admin"], c)
        print(f"  coupon {c['code']:10s} -> {st}")

    # --- Guest wishlist + favourites --------------------------------------
    st, coll = api("POST", "/wishlist/collections", tokens["guest"], {"name": "Europe 2026"})
    coll_id = coll.get("id") if isinstance(coll, dict) else None
    print(f"  collection 'Europe 2026' -> {st}")
    if created:
        api("POST", "/favorites", tokens["guest"], {"propertyId": created[0]["id"], "collectionId": coll_id})
        if len(created) > 1:
            api("POST", "/favorites", tokens["guest"], {"propertyId": created[1]["id"]})
        if len(created) > 2:
            api("POST", "/favorites", tokens["guest"], {"propertyId": created[2]["id"], "collectionId": coll_id})
        print(f"  favourites added for guest")

    # --- Conversation host<->guest ----------------------------------------
    if created:
        st, conv = api("POST", "/conversations", tokens["guest"], {"propertyId": created[1]["id"]})
        if isinstance(conv, dict) and conv.get("id"):
            cid = conv["id"]
            api("POST", f"/conversations/{cid}/messages", tokens["guest"], {"body": "Hi! Is early check-in possible?"})
            api("POST", f"/conversations/{cid}/messages", tokens["host"], {"body": "Hello — yes, from 1pm. Looking forward to hosting you!"})
            print(f"  conversation {cid} seeded with 2 messages")

    # --- Live bookings (guest) --------------------------------------------
    import datetime
    today = datetime.date.today()
    def d(n):
        return (today + datetime.timedelta(days=n)).isoformat()
    if created:
        # Instant-book listing auto-confirms; the other stays pending.
        for prop, ci, co, coupon in (
            (created[0], d(20), d(24), "WELCOME10"),
            (created[1], d(40), d(43), None),
        ):
            body = {"propertyId": prop["id"], "checkIn": ci, "checkOut": co, "guests": 2}
            if coupon:
                body["couponCode"] = coupon
            st, bk = api("POST", "/bookings", tokens["guest"], body)
            status = bk.get("status") if isinstance(bk, dict) else bk
            print(f"  booking on {prop['title'][:24]:24s} {ci}->{co} coupon={coupon} -> {st} ({status})")

    print("\nDone. Created", len(created), "listings.")
    # Emit IDs a follow-up SQL step can use if needed.
    print("PROPERTY_IDS=" + ",".join(c["id"] for c in created))


if __name__ == "__main__":
    main()
