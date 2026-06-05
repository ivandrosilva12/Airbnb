import { useEffect, useState } from 'react';
import { View, Text, Image, ScrollView, TextInput, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

// TODO(i18n): translate — wrap hardcoded labels via useT() from ../i18n/I18nContext.
// Key namespace mirrors the web (detail.*, review.*, policy.*, arrival.*).

export default function PropertyScreen({ route, navigation }) {
  const { id } = route.params;
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [property, setProperty] = useState(null);
  const [reviews, setReviews] = useState([]);
  const [summary, setSummary] = useState(null);
  const [checkIn, setCheckIn] = useState('');
  const [checkOut, setCheckOut] = useState('');
  const [guests, setGuests] = useState('1');
  const [coupon, setCoupon] = useState('');
  const [couponInfo, setCouponInfo] = useState(null);
  // S133 — inline coupon error (separate from the page-level `error` so an
  // invalid promo code doesn't blank out the booking form's own errors).
  // Mirrors web PropertyDetail's `couponError` state.
  const [couponError, setCouponError] = useState(null);
  const [couponLoading, setCouponLoading] = useState(false);
  const [myId, setMyId] = useState(null);
  const [respondingId, setRespondingId] = useState(null);
  const [respondText, setRespondText] = useState('');
  const [editingId, setEditingId] = useState(null);
  const [editDraft, setEditDraft] = useState({ rating: 5, comment: '', categories: {} });
  const [reportingReviewId, setReportingReviewId] = useState(null);
  const [reviewReportReason, setReviewReportReason] = useState('inappropriate');
  const [reportedReviews, setReportedReviews] = useState({});
  const [message, setMessage] = useState(null);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState(null);
  // stepUp holds the structured details when the backend rejects a high-value
  // booking with code=kyc_step_up_required. We render a dedicated panel with
  // the threshold + a CTA to the Verification screen instead of dumping the
  // generic error string — same UX contract as the web PropertyDetail.
  const [stepUp, setStepUp] = useState(null);
  const [reportOpen, setReportOpen] = useState(false);
  const [reportReason, setReportReason] = useState('inappropriate');
  const [reportNote, setReportNote] = useState('');
  const [reported, setReported] = useState(false);
  // S64 — house rules + per-jurisdiction tax preview, mirroring web S56/S57.
  // houseRules is null until fetched and { version, rules: [], updatedAt }
  // once loaded. rulesAccepted is the explicit-consent toggle below the
  // book button; required when houseRules.version > 0 (server gate).
  const [houseRules, setHouseRules] = useState(null);
  const [rulesAccepted, setRulesAccepted] = useState(false);
  const [taxQuote, setTaxQuote] = useState(null);

  useEffect(() => {
    api.getProperty(id).then(setProperty).catch((e) => setError(e.message));
    api.getReviews(id).then((r) => setReviews(r.items || [])).catch(() => {});
    api.getReviewSummary(id).then(setSummary).catch(() => {});
    // S64 — load house rules so the panel renders alongside the listing.
    // 404 (no rules configured) is normal — swallow it.
    api.getHouseRules(id).then(setHouseRules).catch(() => setHouseRules(null));
  }, [id]);

  // S64 — refresh the tax preview whenever the inputs change. We use a
  // cancellation flag so an out-of-order response doesn't clobber the
  // latest one (e.g. the user types a new date before the previous fetch
  // resolved). Stops fetching while inputs are incomplete to keep the
  // initial render clean.
  useEffect(() => {
    if (!property || !checkIn || !checkOut) {
      setTaxQuote(null);
      return undefined;
    }
    const inDate = new Date(checkIn);
    const outDate = new Date(checkOut);
    const nights = Math.max(0, Math.round((outDate - inDate) / (1000 * 60 * 60 * 24)));
    if (nights < 1) {
      setTaxQuote(null);
      return undefined;
    }
    const guestsN = Math.max(1, Number(guests) || 1);
    const subtotalCents = (property.pricePerNight?.amountCents || 0) * nights;
    let cancelled = false;
    api
      .getTaxQuote(id, { checkIn, nights, guests: guestsN, subtotalCents })
      .then((q) => { if (!cancelled) setTaxQuote(q); })
      .catch(() => { if (!cancelled) setTaxQuote(null); });
    return () => { cancelled = true; };
  }, [id, checkIn, checkOut, guests, property]);

  const REVIEW_CATS = ['cleanliness', 'accuracy', 'communication', 'location', 'checkIn', 'value'];
  const CAT_LABELS = {
    cleanliness: 'Cleanliness', accuracy: 'Accuracy', communication: 'Communication',
    location: 'Location', checkIn: 'Check-in', value: 'Value',
  };

  async function applyCoupon() {
    // S133 — clear coupon-specific state only; don't clobber the page-level
    // `error` (e.g. a step-up or rules acceptance message remains visible).
    setCouponError(null);
    setCouponInfo(null);
    if (!authenticated) {
      login();
      return;
    }
    if (!checkIn || !checkOut) {
      // TODO(i18n)
      setCouponError('Choose your dates first.');
      return;
    }
    const code = coupon.trim();
    if (!code) {
      return;
    }
    setCouponLoading(true);
    try {
      const res = await api.previewCoupon({ propertyId: id, checkIn, checkOut, code });
      setCouponInfo(res);
    } catch (e) {
      // TODO(i18n) — server already returns a localized message, but the
      // fallback below keeps the UI sane if the server returns nothing.
      setCouponError(e.message || 'Coupon code is invalid or expired');
    } finally {
      setCouponLoading(false);
    }
  }

  // S133 — clear an applied coupon and re-show the un-discounted preview.
  // The pricing-preview comes from local computation (subtotal × nights), so
  // dropping `couponInfo` is enough to restore the original breakdown.
  function removeCoupon() {
    setCoupon('');
    setCouponInfo(null);
    setCouponError(null);
  }

  // Reflect whether this listing is already in the wishlist so the heart is
  // correct on revisit (rather than always starting empty).
  useEffect(() => {
    if (!authenticated) return;
    api
      .listFavorites()
      .then((res) => setSaved((res.items || []).some((p) => p.id === id)))
      .catch(() => {});
    api.me().then((u) => setMyId(u.id)).catch(() => {});
  }, [id, authenticated]);

  const isOwner = !!(property && myId && property.hostId === myId);

  async function submitResponse(reviewId) {
    setError(null);
    try {
      const updated = await api.respondToReview(reviewId, respondText.trim());
      setReviews((prev) => prev.map((r) => (r.id === updated.id ? updated : r)));
      setRespondingId(null);
      setRespondText('');
    } catch (e) {
      setError(e.message);
    }
  }

  // The author may edit/delete their own review for 48h after posting (mirrors
  // the backend's review.EditWindow).
  const REVIEW_EDIT_WINDOW_MS = 48 * 60 * 60 * 1000;
  function canEditReview(r) {
    return !!(myId && r.authorId === myId) && Date.now() - Date.parse(r.createdAt) <= REVIEW_EDIT_WINDOW_MS;
  }

  async function submitEdit(reviewId) {
    setError(null);
    try {
      // Preserve any existing category ratings, since the backend overwrites them.
      const updated = await api.editReview(reviewId, {
        rating: Number(editDraft.rating) || 1,
        comment: editDraft.comment,
        categories: editDraft.categories || {},
      });
      setReviews((prev) => prev.map((r) => (r.id === updated.id ? updated : r)));
      setEditingId(null);
    } catch (e) {
      setError(e.message);
    }
  }

  async function deleteReview(reviewId) {
    setError(null);
    try {
      await api.deleteReview(reviewId);
      setReviews((prev) => prev.filter((r) => r.id !== reviewId));
    } catch (e) {
      setError(e.message);
    }
  }

  async function submitReviewReport(reviewId) {
    if (!authenticated) {
      login();
      return;
    }
    setError(null);
    try {
      await api.reportReview(reviewId, { reason: reviewReportReason, note: '' });
      setReportedReviews((m) => ({ ...m, [reviewId]: true }));
      setReportingReviewId(null);
    } catch (e) {
      setError(e.message);
    }
  }

  async function save() {
    if (!authenticated) {
      login();
      return;
    }
    try {
      if (saved) {
        await api.removeFavorite(id);
        setSaved(false);
      } else {
        await api.addFavorite(id);
        setSaved(true);
      }
    } catch (e) {
      setError(e.message);
    }
  }

  async function contactHost() {
    if (!authenticated) {
      login();
      return;
    }
    try {
      const conv = await api.startConversation(id);
      navigation.navigate('Conversation', { id: conv.id, title: property?.title || 'Conversation' });
    } catch (e) {
      setError(e.message);
    }
  }

  async function submitReport() {
    if (!authenticated) {
      login();
      return;
    }
    setError(null);
    try {
      await api.reportListing(id, { reason: reportReason, note: reportNote });
      setReported(true);
      setReportOpen(false);
    } catch (e) {
      setError(e.message);
    }
  }

  async function book() {
    setError(null);
    setMessage(null);
    setStepUp(null);
    if (!authenticated) {
      login();
      return;
    }
    // S64 — client-side gate matching the server's house-rules check. The
    // server would 422 anyway, but this short-circuit gives a clearer
    // message than the generic validation error string.
    if (houseRules && houseRules.version > 0 && !rulesAccepted) {
      setError('Please accept the house rules to continue.');
      return;
    }
    try {
      const b = await api.createBooking({
        propertyId: id, checkIn, checkOut, guests: Number(guests),
        couponCode: coupon.trim() || undefined,
        // Only send the field when the listing actually has rules — sending
        // a zero would still let the server detect a missing acknowledgement,
        // but omitting it keeps the payload tidy for rule-less listings.
        ...(houseRules && houseRules.version > 0
          ? { acceptedHouseRulesVersion: houseRules.version }
          : {}),
      });
      setMessage(`Booked ${b.nights} night(s) for ${b.totalPrice.display}. Status: ${b.status}.`);
    } catch (e) {
      // High-value step-up: surface a dedicated verify-and-retry panel using
      // the structured details the backend ships in the response envelope,
      // instead of the raw error string.
      if (e.code === 'kyc_step_up_required') {
        setStepUp(e.details || {});
        return;
      }
      setError(e.message);
    }
  }

  if (!property) {
    return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;
  }

  return (
    <ScrollView style={styles.container}>
      {property.photos?.[0]?.url && <Image source={{ uri: property.photos[0].url }} style={styles.hero} />}
      <View style={styles.body}>
        <Text style={styles.title}>{property.title}</Text>
        <Text style={styles.meta}>{property.address.city}, {property.address.country} · up to {property.maxGuests} guests</Text>
        {summary && summary.count > 0 && (
          <Text style={styles.rating}>★ {summary.averageRating.toFixed(1)} · {summary.count} review(s)</Text>
        )}
        {property.hostIsSuperhost && <Text style={styles.superhost}>★ Superhost</Text>}
        <Text style={styles.price}>{property.pricePerNight.display} / night</Text>
        {property.instantBook && <Text style={styles.instant}>⚡ Instant Book — confirmed instantly</Text>}
        <Text style={styles.desc}>{property.description || 'No description provided.'}</Text>

        <View style={styles.bookBox} accessibilityRole="none">
          <Text style={styles.bookTitle} accessibilityRole="header">
            {property.instantBook ? 'Book instantly' : 'Reserve'}
          </Text>
          <TextInput
            style={styles.input}
            placeholder="Check in (YYYY-MM-DD)"
            value={checkIn}
            onChangeText={setCheckIn}
            accessibilityLabel="Check-in date"
            accessibilityHint="Format: year-month-day, e.g. 2026-07-04"
          />
          <TextInput
            style={styles.input}
            placeholder="Check out (YYYY-MM-DD)"
            value={checkOut}
            onChangeText={setCheckOut}
            accessibilityLabel="Check-out date"
            accessibilityHint="Format: year-month-day"
          />
          <TextInput
            style={styles.input}
            placeholder="Guests"
            keyboardType="number-pad"
            value={guests}
            onChangeText={setGuests}
            accessibilityLabel="Number of guests"
          />
          {/* S133 — coupon code entry. Uppercase-only (codes are stored
              case-insensitively but normalise to upper on the wire), 32
              chars matches the backend cap. When a coupon is already
              applied we lock the input + swap Apply for Remove so the
              guest can't double-submit. */}
          <View style={styles.couponRow}>
            <TextInput
              style={[styles.input, { flex: 1, marginBottom: 0 }, !!couponInfo && styles.inputDisabled]}
              placeholder="Promo code" // TODO(i18n)
              autoCapitalize="characters"
              autoCorrect={false}
              maxLength={32}
              editable={!couponInfo && !couponLoading}
              value={coupon}
              onChangeText={(v) => {
                // Force-uppercase so the visible value mirrors what we'll
                // POST. Mirrors the web autoCapitalize behaviour for codes.
                setCoupon(v.toUpperCase());
                setCouponError(null);
              }}
              accessibilityLabel="Promo code" // TODO(i18n)
            />
            {couponInfo ? (
              <Pressable
                style={styles.couponBtn}
                onPress={removeCoupon}
                accessibilityRole="button"
                accessibilityLabel="Remove promo code" // TODO(i18n)
              >
                <Text style={styles.secondaryText}>Remove{/* TODO(i18n) */}</Text>
              </Pressable>
            ) : (
              <Pressable
                style={styles.couponBtn}
                onPress={applyCoupon}
                disabled={!coupon.trim() || couponLoading}
                accessibilityRole="button"
                accessibilityLabel="Apply promo code" // TODO(i18n)
                accessibilityState={{ disabled: !coupon.trim() || couponLoading, busy: couponLoading }}
              >
                <Text style={styles.secondaryText}>
                  {couponLoading ? 'Applying…' : 'Apply'/* TODO(i18n) */}
                </Text>
              </Pressable>
            )}
          </View>
          {/* S133 — discount line, styled to read as part of the price
              breakdown so the guest can see the new total before they
              tap Reserve. We render the saved amount with a leading minus
              to mirror the web `bd-discount` row. */}
          {couponInfo && (
            <View style={styles.discountRow} accessibilityRole="text">
              <Text style={styles.discountLabel}>
                {/* TODO(i18n) */}
                Discount ({couponInfo.code})
              </Text>
              <Text style={styles.discountVal} accessibilityLabel={`Discount minus ${couponInfo.discount.display}`}>
                −{couponInfo.discount.display}
              </Text>
            </View>
          )}
          {couponInfo && (
            <Text style={styles.success} accessibilityRole="alert">
              {/* TODO(i18n) */}
              Coupon applied — you save {couponInfo.discount.display}.
            </Text>
          )}
          {couponError && (
            <Text style={styles.couponError} accessibilityRole="alert">
              {/* TODO(i18n) */}
              {couponError}
            </Text>
          )}
          {/* S64 — per-jurisdiction tax breakdown. Renders only when we
              actually have rules matching this stay; an empty result
              (anonymous listing in a no-tax jurisdiction) shows nothing. */}
          {taxQuote && taxQuote.lines && taxQuote.lines.length > 0 && (
            <View style={styles.taxBox} accessibilityRole="none">
              <Text style={styles.taxTitle}>Taxes</Text>
              {taxQuote.lines.map((line) => (
                <View key={line.ruleId || line.name} style={styles.taxRow}>
                  <Text style={styles.taxName}>{line.name}</Text>
                  <Text style={styles.taxVal}>{(line.amountCents / 100).toFixed(2)} {taxQuote.currency || ''}</Text>
                </View>
              ))}
              <Text style={styles.taxHint}>Included in the total when you confirm.</Text>
            </View>
          )}
          {/* S64 — house rules preview + acceptance gate. Renders before
              the book button so a guest can't tap-through without
              acknowledging. Only shown when the listing has an active
              ruleset. */}
          {houseRules && houseRules.version > 0 && (
            <View style={styles.rulesBox} accessibilityRole="none">
              <Text style={styles.rulesTitle}>House rules</Text>
              {(houseRules.rules || []).map((rule, i) => (
                <Text key={i} style={styles.rulesItem}>• {rule}</Text>
              ))}
              <Pressable
                style={styles.acceptRow}
                onPress={() => setRulesAccepted((v) => !v)}
                accessibilityRole="checkbox"
                accessibilityState={{ checked: rulesAccepted }}
                accessibilityLabel="Accept the house rules"
              >
                <Text style={rulesAccepted ? styles.acceptOn : styles.acceptOff}>{rulesAccepted ? '☑' : '☐'}</Text>
                <Text style={styles.acceptLabel}>I accept the house rules</Text>
              </Pressable>
            </View>
          )}
          <Pressable
            style={styles.btn}
            onPress={book}
            accessibilityRole="button"
            accessibilityLabel={!authenticated ? 'Sign in to reserve' : property.instantBook ? 'Book instantly' : 'Send reservation request'}
            accessibilityHint={!authenticated ? 'Opens the sign-in flow' : property.instantBook ? 'Confirms the booking immediately' : 'Sends the request to the host for approval'}
          >
            <Text style={styles.btnText}>{!authenticated ? 'Sign in to reserve' : property.instantBook ? '⚡ Book instantly' : 'Reserve'}</Text>
          </Pressable>
          {message && <Text style={styles.success}>{message}</Text>}
          {stepUp && (
            <View style={styles.kycPanel}>
              <Text style={styles.kycTitle}>Identity verification needed</Text>
              <Text style={styles.kycHint}>
                Bookings of {((stepUp.thresholdCents || 0) / 100).toFixed(0)} {stepUp.currency || ''} or more require a verified identity. Verify once and you can complete this reservation right after.
              </Text>
              <Pressable style={styles.btn} onPress={() => navigation.navigate('Verification')}>
                <Text style={styles.btnText}>Verify my identity</Text>
              </Pressable>
            </View>
          )}
          {error && <Text style={styles.error}>{error}</Text>}
        </View>

        <View style={styles.secondaryActions}>
          <Pressable
            style={styles.secondaryBtn}
            onPress={save}
            accessibilityRole="button"
            accessibilityLabel={saved ? 'Remove from wishlist' : 'Save to wishlist'}
            accessibilityState={{ selected: saved }}
          >
            <Text style={styles.secondaryText}>{saved ? '♥ Saved' : '♡ Save to wishlist'}</Text>
          </Pressable>
          <Pressable
            style={styles.secondaryBtn}
            onPress={contactHost}
            accessibilityRole="button"
            accessibilityLabel="Contact the host"
            accessibilityHint="Opens a new conversation with the host of this listing"
          >
            <Text style={styles.secondaryText}>Contact host</Text>
          </Pressable>
        </View>

        <View style={styles.reviewsSection}>
          <Text style={styles.sectionTitle}>Reviews</Text>
          {summary?.categories && (
            <View style={styles.catBreakdown}>
              {REVIEW_CATS.filter((k) => summary.categories[k] > 0).map((k) => (
                <View key={k} style={styles.catRow}>
                  <Text style={styles.catLabel}>{CAT_LABELS[k]}</Text>
                  <Text style={styles.catVal}>{summary.categories[k].toFixed(1)}</Text>
                </View>
              ))}
            </View>
          )}
          {reviews.length === 0 ? (
            <Text style={styles.meta}>No reviews yet.</Text>
          ) : (
            reviews.map((r) => (
              <View key={r.id} style={styles.reviewItem}>
                <Text style={styles.reviewStars}>★ {r.rating}{r.updatedAt ? ' · edited' : ''}</Text>
                {editingId === r.id ? (
                  <View style={styles.respondBox}>
                    <View style={styles.starRow}>
                      {[1, 2, 3, 4, 5].map((n) => (
                        <Pressable key={n} onPress={() => setEditDraft((d) => ({ ...d, rating: n }))}>
                          <Text style={n <= editDraft.rating ? styles.starOn : styles.starOff}>★</Text>
                        </Pressable>
                      ))}
                    </View>
                    <TextInput
                      style={[styles.input, { marginBottom: 8 }]}
                      placeholder="Update your review…"
                      value={editDraft.comment}
                      onChangeText={(v) => setEditDraft((d) => ({ ...d, comment: v }))}
                      multiline
                    />
                    <View style={styles.secondaryActions}>
                      <Pressable style={styles.btn} onPress={() => submitEdit(r.id)}>
                        <Text style={styles.btnText}>Save</Text>
                      </Pressable>
                      <Pressable style={styles.secondaryBtn} onPress={() => setEditingId(null)}>
                        <Text style={styles.secondaryText}>Cancel</Text>
                      </Pressable>
                    </View>
                  </View>
                ) : (
                  !!r.comment && <Text style={styles.reviewComment}>{r.comment}</Text>
                )}
                {editingId !== r.id && r.categories && (
                  <Text style={styles.reviewCats}>
                    {REVIEW_CATS.filter((k) => r.categories[k] > 0).map((k) => `${CAT_LABELS[k]} ${r.categories[k]}`).join(' · ')}
                  </Text>
                )}
                {editingId !== r.id && canEditReview(r) && (
                  <View style={styles.secondaryActions}>
                    <Pressable onPress={() => { setEditDraft({ rating: r.rating, comment: r.comment, categories: r.categories || {} }); setEditingId(r.id); }}>
                      <Text style={styles.respondLink}>Edit</Text>
                    </Pressable>
                    <Pressable onPress={() => deleteReview(r.id)}>
                      <Text style={styles.reportLink}>Delete</Text>
                    </Pressable>
                  </View>
                )}
                {editingId !== r.id && !(myId && r.authorId === myId) && (
                  reportedReviews[r.id] ? (
                    <Text style={styles.reportDone}>Thanks — our team will take a look.</Text>
                  ) : reportingReviewId === r.id ? (
                    <View style={styles.respondBox}>
                      <View style={styles.reasonRow}>
                        {REPORT_REASONS.map((rr) => (
                          <Pressable
                            key={rr.value}
                            style={[styles.reasonChip, reviewReportReason === rr.value && styles.reasonChipActive]}
                            onPress={() => setReviewReportReason(rr.value)}
                          >
                            <Text style={reviewReportReason === rr.value ? styles.reasonChipTextActive : styles.reasonChipText}>{rr.label}</Text>
                          </Pressable>
                        ))}
                      </View>
                      <View style={styles.secondaryActions}>
                        <Pressable style={styles.btn} onPress={() => submitReviewReport(r.id)}>
                          <Text style={styles.btnText}>Submit report</Text>
                        </Pressable>
                        <Pressable style={styles.secondaryBtn} onPress={() => setReportingReviewId(null)}>
                          <Text style={styles.secondaryText}>Cancel</Text>
                        </Pressable>
                      </View>
                    </View>
                  ) : (
                    <Pressable onPress={() => { setReviewReportReason('inappropriate'); setReportingReviewId(r.id); }}>
                      <Text style={styles.reportLink}>⚑ Report review</Text>
                    </Pressable>
                  )
                )}
                {!!r.response && (
                  <View style={styles.reviewResponse}>
                    <Text style={styles.reviewResponseLabel}>Host response</Text>
                    <Text style={styles.reviewComment}>{r.response}</Text>
                  </View>
                )}
                {isOwner && !r.response && (
                  respondingId === r.id ? (
                    <View style={styles.respondBox}>
                      <TextInput
                        style={[styles.input, { marginBottom: 8 }]}
                        placeholder="Write a public reply…"
                        value={respondText}
                        onChangeText={setRespondText}
                        multiline
                      />
                      <View style={styles.secondaryActions}>
                        <Pressable style={styles.btn} onPress={() => submitResponse(r.id)}>
                          <Text style={styles.btnText}>Post response</Text>
                        </Pressable>
                        <Pressable style={styles.secondaryBtn} onPress={() => { setRespondingId(null); setRespondText(''); }}>
                          <Text style={styles.secondaryText}>Cancel</Text>
                        </Pressable>
                      </View>
                    </View>
                  ) : (
                    <Pressable onPress={() => { setRespondingId(r.id); setRespondText(''); }}>
                      <Text style={styles.respondLink}>Respond</Text>
                    </Pressable>
                  )
                )}
              </View>
            ))
          )}
        </View>

        {reported ? (
          <Text style={styles.reportDone} accessibilityRole="alert">Thanks — our team will review this listing.</Text>
        ) : !reportOpen ? (
          <Pressable
            onPress={() => setReportOpen(true)}
            accessibilityRole="button"
            accessibilityLabel="Report this listing"
            accessibilityHint="Opens a form to flag the listing for moderation"
          >
            <Text style={styles.reportLink}>⚑ Report listing</Text>
          </Pressable>
        ) : (
          <View style={styles.reportBox}>
            <Text style={styles.bookTitle}>Report this listing</Text>
            <View style={styles.reasonRow}>
              {REPORT_REASONS.map((r) => (
                <Pressable
                  key={r.value}
                  style={[styles.reasonChip, reportReason === r.value && styles.reasonChipActive]}
                  onPress={() => setReportReason(r.value)}
                >
                  <Text style={reportReason === r.value ? styles.reasonChipTextActive : styles.reasonChipText}>{r.label}</Text>
                </Pressable>
              ))}
            </View>
            <TextInput
              style={[styles.input, { marginTop: 10 }]}
              placeholder="Details (optional)"
              value={reportNote}
              onChangeText={setReportNote}
              multiline
            />
            <View style={styles.secondaryActions}>
              <Pressable style={styles.btn} onPress={submitReport}>
                <Text style={styles.btnText}>Submit report</Text>
              </Pressable>
              <Pressable style={styles.secondaryBtn} onPress={() => setReportOpen(false)}>
                <Text style={styles.secondaryText}>Cancel</Text>
              </Pressable>
            </View>
          </View>
        )}
      </View>
    </ScrollView>
  );
}

const REPORT_REASONS = [
  { value: 'spam', label: 'Spam' },
  { value: 'inappropriate', label: 'Inappropriate' },
  { value: 'scam', label: 'Scam' },
  { value: 'inaccurate', label: 'Inaccurate' },
  { value: 'other', label: 'Other' },
];

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  hero: { width: '100%', height: 240 },
  body: { padding: 16 },
  title: { fontSize: 22, fontWeight: '800' },
  meta: { color: '#717171', marginVertical: 4 },
  rating: { fontWeight: '700', color: '#222', marginVertical: 2 },
  price: { fontSize: 16, fontWeight: '600', marginVertical: 4 },
  instant: { color: '#ff385c', fontWeight: '700', marginTop: 2 },
  superhost: { color: '#222', fontWeight: '700', marginVertical: 2 },
  desc: { marginVertical: 12, lineHeight: 20 },
  bookBox: { borderWidth: 1, borderColor: '#ddd', borderRadius: 12, padding: 16, marginTop: 8 },
  bookTitle: { fontWeight: '700', fontSize: 16, marginBottom: 10 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 10, marginBottom: 10 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 12, alignItems: 'center' },
  btnText: { color: '#fff', fontWeight: '700' },
  success: { color: '#1a7f47', marginTop: 10 },
  error: { color: '#c0392b', marginTop: 10 },
  kycPanel: { marginTop: 12, backgroundColor: '#fff5e6', borderRadius: 8, padding: 12, borderWidth: 1, borderColor: '#f5c97a' },
  kycTitle: { fontWeight: '800', color: '#8a5d00', marginBottom: 6 },
  kycHint: { color: '#5a3f00', lineHeight: 19, marginBottom: 10 },
  secondaryActions: { flexDirection: 'row', gap: 10, marginTop: 12, alignItems: 'center' },
  starRow: { flexDirection: 'row', alignItems: 'center', gap: 4, marginBottom: 8 },
  starOn: { color: '#ff385c', fontSize: 26 },
  starOff: { color: '#ddd', fontSize: 26 },
  secondaryBtn: { flex: 1, borderWidth: 1, borderColor: '#ddd', borderRadius: 8, paddingVertical: 12, alignItems: 'center' },
  secondaryText: { fontWeight: '600', color: '#222' },
  couponRow: { flexDirection: 'row', gap: 8, marginBottom: 10 },
  couponBtn: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, paddingHorizontal: 16, justifyContent: 'center' },
  // S133 — coupon-applied input lock + discount breakdown styling.
  inputDisabled: { backgroundColor: '#f5f5f5', color: '#717171' },
  discountRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 4, marginBottom: 4 },
  discountLabel: { color: '#1a7f47', fontWeight: '600' },
  discountVal: { color: '#1a7f47', fontWeight: '700' },
  couponError: { color: '#c0392b', marginTop: 6, marginBottom: 6 },
  // S64 — house rules + tax preview styles, kept minimal to match the
  // visual weight of the existing bookBox.
  taxBox: { backgroundColor: '#fafafa', borderRadius: 8, padding: 10, marginBottom: 10 },
  taxTitle: { fontWeight: '700', color: '#222', marginBottom: 6 },
  taxRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 2 },
  taxName: { color: '#222' },
  taxVal: { color: '#222', fontWeight: '600' },
  taxHint: { color: '#717171', fontSize: 12, marginTop: 6 },
  rulesBox: { backgroundColor: '#fff5e6', borderRadius: 8, padding: 12, marginBottom: 10, borderWidth: 1, borderColor: '#f5c97a' },
  rulesTitle: { fontWeight: '800', color: '#8a5d00', marginBottom: 6 },
  rulesItem: { color: '#5a3f00', lineHeight: 20 },
  acceptRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 10 },
  acceptOn: { color: '#ff385c', fontSize: 22 },
  acceptOff: { color: '#999', fontSize: 22 },
  acceptLabel: { color: '#222', fontWeight: '600' },
  reviewsSection: { marginTop: 20 },
  sectionTitle: { fontSize: 18, fontWeight: '800', marginBottom: 8 },
  catBreakdown: { backgroundColor: '#fafafa', borderRadius: 8, padding: 12, marginBottom: 12 },
  catRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 2 },
  catLabel: { color: '#717171' },
  catVal: { color: '#222', fontWeight: '600' },
  reviewItem: { borderTopWidth: 1, borderColor: '#eee', paddingVertical: 10 },
  reviewStars: { fontWeight: '700', color: '#222' },
  reviewComment: { color: '#222', marginTop: 2, lineHeight: 19 },
  reviewCats: { color: '#717171', fontSize: 12, marginTop: 4 },
  reviewResponse: { marginTop: 8, marginLeft: 12, paddingLeft: 10, borderLeftWidth: 3, borderColor: '#eee' },
  reviewResponseLabel: { fontSize: 12, color: '#717171', fontWeight: '700' },
  respondBox: { marginTop: 8 },
  respondLink: { color: '#ff385c', fontWeight: '700', marginTop: 6 },
  reportLink: { color: '#c0392b', textDecorationLine: 'underline', marginTop: 16 },
  reportDone: { color: '#1a7f47', marginTop: 16 },
  reportBox: { borderWidth: 1, borderColor: '#eee', borderRadius: 12, padding: 16, marginTop: 16 },
  reasonRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  reasonChip: { borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 12, paddingVertical: 6 },
  reasonChipActive: { backgroundColor: '#ff385c', borderColor: '#ff385c' },
  reasonChipText: { color: '#222', fontSize: 13 },
  reasonChipTextActive: { color: '#fff', fontWeight: '700', fontSize: 13 },
});
