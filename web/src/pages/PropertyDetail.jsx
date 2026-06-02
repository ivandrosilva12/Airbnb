import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useFavorites } from '../context/FavoritesContext';
import { useT } from '../i18n/I18nContext';
import AvailabilityCalendar from '../components/AvailabilityCalendar';
import { REVIEW_CATEGORIES } from './MyTrips';

function bookedDaySet(booked) {
  const set = new Set();
  for (const r of booked) {
    const start = new Date(`${r.checkIn}T00:00:00Z`);
    const end = new Date(`${r.checkOut}T00:00:00Z`);
    for (let d = new Date(start); d < end; d.setUTCDate(d.getUTCDate() + 1)) {
      set.add(d.toISOString().slice(0, 10));
    }
  }
  return set;
}

function rangeHitsBooked(checkIn, checkOut, set) {
  const start = new Date(`${checkIn}T00:00:00Z`);
  const end = new Date(`${checkOut}T00:00:00Z`);
  for (let d = new Date(start); d < end; d.setUTCDate(d.getUTCDate() + 1)) {
    if (set.has(d.toISOString().slice(0, 10))) return true;
  }
  return false;
}

export default function PropertyDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { authenticated, profile, login } = useAuth();
  const { isFavorite, toggle } = useFavorites();
  const { t } = useT();
  const [property, setProperty] = useState(null);
  const [reviews, setReviews] = useState([]);
  const [summary, setSummary] = useState(null);
  const [booked, setBooked] = useState([]);
  const [form, setForm] = useState({ checkIn: '', checkOut: '', guests: 1 });
  const [coupon, setCoupon] = useState('');
  const [couponInfo, setCouponInfo] = useState(null);
  const [couponError, setCouponError] = useState(null);
  const [message, setMessage] = useState(null);
  const [error, setError] = useState(null);
  // stepUp holds the structured details when the backend rejects the booking
  // because the total exceeds the high-value threshold and the guest is not
  // yet verified. When set, the UI swaps the generic error for a "verify and
  // come back" prompt with a direct link to /settings.
  const [stepUp, setStepUp] = useState(null);
  // House rules (S56): the public-read snapshot for this listing + the
  // guest's "I've read and accept" checkbox. The acceptance MUST submit
  // with the booking — the backend version-checks server-side so a
  // stale UI (rules bumped between page-load and submit) is rejected
  // with the standard validation error.
  const [houseRules, setHouseRules] = useState({ version: 0, items: [] });
  const [rulesAccepted, setRulesAccepted] = useState(false);
  // Tax preview (S57): per-jurisdiction line items the booking total
  // will include. Fetched whenever the stay shape changes; the
  // booking response already reflects the same lines server-side
  // (S49), so the breakdown matches what the guest is charged.
  const [taxQuote, setTaxQuote] = useState(null);

  useEffect(() => {
    api.getProperty(id).then(setProperty).catch((e) => setError(e.message));
    api.getReviews(id).then((r) => setReviews(r.items || [])).catch(() => {});
    api.getReviewSummary(id).then(setSummary).catch(() => {});
    api.availability(id).then((a) => setBooked(a.booked || [])).catch(() => {});
    api.getHouseRules(id).then(setHouseRules).catch(() => {});
  }, [id]);

  // Tax-quote refetch (S57). The taxable base is the same expression
  // the server's calculator uses: subtotal net of discount + cleaning
  // + extra-guest (per S49's quoteJurisdictionTax). Quote silently
  // becomes null when inputs aren't ready — the price-breakdown skips
  // the lines block without flickering "loading".
  useEffect(() => {
    if (!property) return;
    if (!form.checkIn || !form.checkOut) {
      setTaxQuote(null);
      return;
    }
    const nights = Math.round((new Date(form.checkOut) - new Date(form.checkIn)) / 86400000);
    if (nights <= 0) {
      setTaxQuote(null);
      return;
    }
    const subtotal = nights * property.pricePerNight.amountCents;
    const cleaning = property.cleaningFee?.amountCents || 0;
    const included = property.guestsIncluded || property.maxGuests;
    const extras = Math.max(0, Number(form.guests || 1) - included);
    const extraFee = extras * (property.extraGuestFee?.amountCents || 0) * nights;
    const couponDisc = couponInfo?.discount?.amountCents || 0;
    const taxableBase = Math.max(0, subtotal - couponDisc) + cleaning + extraFee;

    let cancelled = false;
    api
      .getTaxQuote(id, {
        checkIn: form.checkIn,
        nights,
        guests: Number(form.guests || 1),
        subtotalCents: taxableBase,
      })
      .then((q) => { if (!cancelled) setTaxQuote(q); })
      .catch(() => { if (!cancelled) setTaxQuote(null); });
    return () => {
      cancelled = true;
    };
  }, [id, property, form.checkIn, form.checkOut, form.guests, couponInfo]);

  async function book(e) {
    e.preventDefault();
    setError(null);
    setMessage(null);
    setStepUp(null);
    if (!authenticated) {
      login();
      return;
    }
    if (rangeHitsBooked(form.checkIn, form.checkOut, bookedDaySet(booked))) {
      setError(t('detail.datesUnavailable'));
      return;
    }
    // House-rules acceptance precondition: when the listing has active
    // rules, the guest MUST tick the box. Local UX guard so we surface
    // a clean message instead of letting the backend return its generic
    // "please accept the current house rules to book" validation.
    if (houseRules.version > 0 && houseRules.items.length > 0 && !rulesAccepted) {
      setError(t('houseRules.mustAccept'));
      return;
    }
    try {
      const booking = await api.createBooking({
        propertyId: id,
        checkIn: form.checkIn,
        checkOut: form.checkOut,
        guests: Number(form.guests),
        couponCode: coupon.trim() || undefined,
        // 0 = no acknowledgement; backend ignores it on listings without
        // active rules and rejects with the version-mismatch error on
        // listings that DO have them.
        acceptedHouseRulesVersion:
          houseRules.version > 0 && houseRules.items.length > 0 ? houseRules.version : undefined,
      });
      setMessage(t('detail.booked', { nights: booking.nights, total: booking.totalPrice.display, status: booking.status }));
      api.availability(id).then((a) => setBooked(a.booked || [])).catch(() => {});
    } catch (e) {
      // High-value step-up: surface a verify-and-retry prompt instead of the
      // raw error message, using the structured details the backend ships in
      // the response envelope.
      if (e.code === 'kyc_step_up_required') {
        setStepUp(e.details || {});
        return;
      }
      setError(e.message);
    }
  }

  async function applyCoupon() {
    setCouponError(null);
    setCouponInfo(null);
    if (!authenticated) {
      login();
      return;
    }
    if (!form.checkIn || !form.checkOut) {
      setCouponError(t('detail.couponNeedDates'));
      return;
    }
    try {
      const res = await api.previewCoupon({
        propertyId: id,
        checkIn: form.checkIn,
        checkOut: form.checkOut,
        code: coupon.trim(),
      });
      setCouponInfo(res);
    } catch (e) {
      setCouponError(e.message);
    }
  }

  async function contactHost() {
    setError(null);
    if (!authenticated) {
      login();
      return;
    }
    try {
      const conv = await api.startConversation(id);
      navigate(`/messages?conversation=${conv.id}`);
    } catch (e) {
      setError(e.message);
    }
  }

  if (error && !property) return <div className="container"><p className="error">{error}</p></div>;
  if (!property) return <div className="container"><p>{t('common.loading')}</p></div>;

  const isOwnListing = profile?.id === property.hostId;

  const currency = property.pricePerNight.currency;
  const fmt = (cents) => `${(cents / 100).toFixed(2)} ${currency}`;
  const nights = (() => {
    if (!form.checkIn || !form.checkOut) return 0;
    const ms = new Date(form.checkOut) - new Date(form.checkIn);
    return ms > 0 ? Math.round(ms / 86400000) : 0;
  })();
  const subtotalCents = nights * property.pricePerNight.amountCents;
  const cleaningCents = property.cleaningFee?.amountCents || 0;
  const discountPct =
    nights >= 28 && property.monthlyDiscountPct > 0
      ? property.monthlyDiscountPct
      : nights >= 7 && property.weeklyDiscountPct > 0
        ? property.weeklyDiscountPct
        : 0;
  const discountCents = Math.round(subtotalCents * discountPct);
  const couponCents = couponInfo?.discount?.amountCents || 0;
  const guestsIncluded = property.guestsIncluded || property.maxGuests;
  const extraGuests = Math.max(0, Number(form.guests || 1) - guestsIncluded);
  const extraGuestCents = extraGuests * (property.extraGuestFee?.amountCents || 0) * nights;
  const depositCents = property.securityDeposit?.amountCents || 0;
  const belowMinNights = nights > 0 && property.minNights > 1 && nights < property.minNights;

  return (
    <div className="container detail">
      <h1>{property.title}</h1>
      <p className="card-meta">
        {property.address.city}, {property.address.country} · {t(`type.${property.type}`)} · {t('detail.upToGuests', { n: property.maxGuests })}
        {summary && summary.count > 0 && ` · ★ ${summary.averageRating.toFixed(1)} (${summary.count})`}
      </p>
      {property.hostIsSuperhost && (
        <p className="superhost-line" title={t('detail.superhostHint')}>★ {t('detail.superhost')}</p>
      )}

      <div className="gallery">
        {property.photos.length === 0 && <div className="card-photo-placeholder">{t('common.noPhoto')}</div>}
        {property.photos.map((ph, i) => (
          <img key={ph.id} src={ph.url} alt={t('a11y.photoOf', { title: property.title, n: i + 1 })} />
        ))}
      </div>

      <div className="detail-grid">
        <div>
          <p>{property.description || t('detail.noDescription')}</p>

          <h3>{t('detail.availability')}</h3>
          <AvailabilityCalendar booked={booked} months={2} />

          <h3>{t('detail.cancellation')}</h3>
          <p className="policy-line">{t(`policy.${property.cancellationPolicy}`)}</p>

          {houseRules.items && houseRules.items.length > 0 && (
            <div className="house-rules-preview">
              <h3>{t('houseRules.guestHeading')}</h3>
              <ul aria-label={t('houseRules.guestHeading')}>
                {houseRules.items.map((rule, i) => (
                  <li key={i}>{rule}</li>
                ))}
              </ul>
              <small className="muted">{t('houseRules.versionLabel', { version: houseRules.version })}</small>
            </div>
          )}

          <h3>{t('detail.amenities')}</h3>
          <ul className="amenities">
            {property.amenities.length === 0 && <li>{t('detail.noAmenities')}</li>}
            {property.amenities.map((a) => (
              <li key={a}>{a}</li>
            ))}
          </ul>

          <h3>{t('detail.reviews')}</h3>
          {summary?.categories && (
            <div className="cat-breakdown">
              <strong>{t('detail.categoryRatings')}</strong>
              {REVIEW_CATEGORIES.filter((k) => summary.categories[k] > 0).map((k) => (
                <div key={k} className="cat-row">
                  <span className="cat-label">{t(`review.cat.${k}`)}</span>
                  <span className="cat-bar"><span className="cat-fill" style={{ width: `${(summary.categories[k] / 5) * 100}%` }} /></span>
                  <span className="cat-num">{summary.categories[k].toFixed(1)}</span>
                </div>
              ))}
            </div>
          )}
          {reviews.length === 0 && <p>{t('detail.noReviews')}</p>}
          {reviews.map((r) => (
            <ReviewItem
              key={r.id}
              review={r}
              canRespond={isOwnListing}
              currentUserId={profile?.id}
              authenticated={authenticated}
              login={login}
              onResponded={(updated) => setReviews((prev) => prev.map((x) => (x.id === updated.id ? updated : x)))}
              onChanged={(updated) => setReviews((prev) => prev.map((x) => (x.id === updated.id ? updated : x)))}
              onDeleted={(deletedId) => setReviews((prev) => prev.filter((x) => x.id !== deletedId))}
            />
          ))}
        </div>

        <aside className="booking-box">
          <div className="booking-price">
            <strong>{property.pricePerNight.display}</strong> {t('common.perNight')}
          </div>
          {property.instantBook && (
            <div className="instant-badge" title={t('detail.instantBookHint')}>⚡ {t('detail.instantBook')}</div>
          )}
          <form onSubmit={book}>
            <label>
              {t('common.checkIn')}
              <input type="date" required value={form.checkIn} onChange={(e) => setForm({ ...form, checkIn: e.target.value })} />
            </label>
            <label>
              {t('common.checkOut')}
              <input type="date" required value={form.checkOut} onChange={(e) => setForm({ ...form, checkOut: e.target.value })} />
            </label>
            <label>
              {t('common.guests')}
              <input type="number" min="1" max={property.maxGuests} value={form.guests} onChange={(e) => setForm({ ...form, guests: e.target.value })} />
            </label>
            {nights > 0 && (
              <div className="price-breakdown">
                <div><span>{t('detail.nights', { price: property.pricePerNight.display, n: nights })}</span><span>{fmt(subtotalCents)}</span></div>
                {discountCents > 0 && <div className="bd-discount"><span>{t('detail.discount', { pct: Math.round(discountPct * 100) })}</span><span>-{fmt(discountCents)}</span></div>}
                {couponCents > 0 && <div className="bd-discount"><span>{t('detail.couponDiscount', { code: couponInfo.code })}</span><span>-{fmt(couponCents)}</span></div>}
                {cleaningCents > 0 && <div><span>{t('detail.cleaningFee')}</span><span>{fmt(cleaningCents)}</span></div>}
                {extraGuestCents > 0 && <div><span>{t('detail.extraGuestFee', { n: extraGuests })}</span><span>{fmt(extraGuestCents)}</span></div>}
                {taxQuote && taxQuote.lines && taxQuote.lines.length > 0 && (
                  <>
                    {taxQuote.lines.map((line) => (
                      <div key={line.ruleId || line.name} className="bd-tax">
                        <span>{line.name}</span>
                        <span>{fmt(line.amountCents)}</span>
                      </div>
                    ))}
                    <div className="muted bd-tax-note">
                      {t('detail.taxesIncludedHint')}
                    </div>
                  </>
                )}
                <div className="muted">{t('detail.serviceNote')}</div>
                <div className="bd-total">
                  <span>{t('detail.beforeFees')}</span>
                  <span>{fmt(
                    Math.max(0, subtotalCents - discountCents - couponCents) +
                      cleaningCents +
                      extraGuestCents +
                      (taxQuote?.totalCents || 0),
                  )}</span>
                </div>
                {depositCents > 0 && <div className="muted"><span>{t('detail.securityDeposit', { amount: fmt(depositCents) })}</span></div>}
              </div>
            )}
            {belowMinNights && <p className="error coupon-msg">{t('detail.minNights', { n: property.minNights })}</p>}
            <div className="coupon-row">
              <input
                placeholder={t('detail.couponPlaceholder')}
                value={coupon}
                aria-label={t('detail.couponPlaceholder')}
                onChange={(e) => { setCoupon(e.target.value); setCouponInfo(null); setCouponError(null); }}
              />
              <button className="btn btn-ghost" type="button" onClick={applyCoupon} disabled={!coupon.trim()}>
                {t('detail.couponApply')}
              </button>
            </div>
            {couponInfo && <p className="success coupon-msg">{t('detail.couponApplied', { discount: couponInfo.discount.display })}</p>}
            {couponError && <p className="error coupon-msg">{couponError}</p>}
            {houseRules.items && houseRules.items.length > 0 && (
              <label className="house-rules-check">
                <input
                  type="checkbox"
                  checked={rulesAccepted}
                  onChange={(e) => setRulesAccepted(e.target.checked)}
                  aria-describedby="house-rules-hint"
                />
                <span>{t('houseRules.acceptLabel', { version: houseRules.version })}</span>
                <small id="house-rules-hint" className="muted">{t('houseRules.acceptHint')}</small>
              </label>
            )}
            <button className="btn btn-primary block" type="submit">
              {!authenticated
                ? t('detail.signInReserve')
                : property.instantBook
                  ? t('detail.bookInstantly')
                  : t('detail.reserve')}
            </button>
          </form>
          {!isOwnListing && (
            <button className="btn btn-ghost block" onClick={contactHost}>{t('detail.contactHost')}</button>
          )}
          {authenticated && !isOwnListing && (
            <button className="btn btn-ghost block" onClick={() => toggle(property.id)}>
              {isFavorite(property.id) ? t('detail.saved') : t('detail.save')}
            </button>
          )}
          {message && <p className="success">{message}</p>}
          {stepUp && (
            <div className="kyc-stepup">
              <strong>{t('kyc.stepUpTitle')}</strong>
              <p>{t('kyc.stepUpHint', {
                amount: ((stepUp.thresholdCents || 0) / 100).toFixed(0),
                currency: stepUp.currency || '',
              })}</p>
              <button className="btn btn-primary" type="button" onClick={() => navigate('/settings')}>
                {t('kyc.stepUpCta')}
              </button>
            </div>
          )}
          {error && <p className="error">{error}</p>}
          {!isOwnListing && (
            <ReportControl
              onReport={(reason, note) => api.reportListing(property.id, { reason, note })}
              authenticated={authenticated}
              login={login}
            />
          )}
        </aside>
      </div>
    </div>
  );
}

// The author may edit/delete their own review for this long after posting.
// Mirrors the backend's review.EditWindow (48h).
const REVIEW_EDIT_WINDOW_MS = 48 * 60 * 60 * 1000;

// ReviewItem renders a guest review and the host's reply. The owning host can
// post one public response inline when a review has none yet, and the review's
// own author can edit or delete it within the edit window.
function ReviewItem({ review: r, canRespond, currentUserId, authenticated, login, onResponded, onChanged, onDeleted }) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState({ rating: r.rating, comment: r.comment, cats: r.categories || {} });

  const isAuthor = currentUserId && r.authorId === currentUserId;
  const withinWindow = Date.now() - Date.parse(r.createdAt) <= REVIEW_EDIT_WINDOW_MS;
  const canEdit = isAuthor && withinWindow;

  async function submit(e) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const updated = await api.respondToReview(r.id, text.trim());
      onResponded(updated);
      setOpen(false);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function saveEdit(e) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const categories = {};
      for (const k of REVIEW_CATEGORIES) categories[k] = Number(draft.cats[k] || 0);
      const updated = await api.editReview(r.id, { rating: Number(draft.rating), comment: draft.comment, categories });
      onChanged(updated);
      setEditing(false);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!window.confirm(t('review.deleteConfirm'))) return;
    setBusy(true);
    setError(null);
    try {
      await api.deleteReview(r.id);
      onDeleted(r.id);
    } catch (err) {
      setError(err.message);
      setBusy(false);
    }
  }

  if (editing) {
    return (
      <form className="review review-edit" onSubmit={saveEdit}>
        <select value={draft.rating} aria-label={t('review.overall')} onChange={(e) => setDraft((d) => ({ ...d, rating: e.target.value }))}>
          {[5, 4, 3, 2, 1].map((n) => <option key={n} value={n}>{'★'.repeat(n)}</option>)}
        </select>
        <textarea rows="2" value={draft.comment} onChange={(e) => setDraft((d) => ({ ...d, comment: e.target.value }))} placeholder={t('trips.shareExperience')} />
        <div className="review-cats">
          {REVIEW_CATEGORIES.map((k) => (
            <label key={k} className="review-cat">
              <span>{t(`review.cat.${k}`)}</span>
              <select value={draft.cats[k] || 0} onChange={(e) => setDraft((d) => ({ ...d, cats: { ...d.cats, [k]: Number(e.target.value) } }))}>
                <option value={0}>—</option>
                {[1, 2, 3, 4, 5].map((n) => <option key={n} value={n}>{n}</option>)}
              </select>
            </label>
          ))}
        </div>
        {error && <p className="error">{error}</p>}
        <div className="report-actions">
          <button className="btn btn-primary" type="submit" disabled={busy}>{busy ? '…' : t('review.saveEdit')}</button>
          <button className="btn btn-ghost" type="button" onClick={() => setEditing(false)}>{t('common.cancel')}</button>
        </div>
      </form>
    );
  }

  return (
    <div className="review">
      <strong>★ {r.rating}</strong>
      {r.updatedAt && <span className="review-edited"> · {t('review.edited')}</span>}
      <p>{r.comment}</p>
      {r.categories && (
        <ul className="review-cat-chips">
          {REVIEW_CATEGORIES.filter((k) => r.categories[k] > 0).map((k) => (
            <li key={k}>{t(`review.cat.${k}`)} <strong>{r.categories[k]}</strong></li>
          ))}
        </ul>
      )}
      {canEdit && (
        <div className="review-owner-actions">
          <button className="btn-link" onClick={() => { setError(null); setDraft({ rating: r.rating, comment: r.comment, cats: r.categories || {} }); setEditing(true); }}>{t('review.edit')}</button>
          <button className="btn-link-danger" onClick={remove} disabled={busy}>{t('review.delete')}</button>
        </div>
      )}
      {error && !editing && <p className="error">{error}</p>}
      {r.response ? (
        <div className="review-response">
          <strong>{t('detail.hostResponse')}</strong>
          <p>{r.response}</p>
        </div>
      ) : canRespond && open ? (
        <form className="review-respond" onSubmit={submit}>
          <textarea
            rows="2"
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={t('detail.respondPlaceholder')}
            required
          />
          {error && <p className="error">{error}</p>}
          <div className="report-actions">
            <button className="btn btn-primary" type="submit" disabled={busy}>
              {busy ? t('detail.responding') : t('detail.respondSubmit')}
            </button>
            <button className="btn btn-ghost" type="button" onClick={() => setOpen(false)}>{t('common.cancel')}</button>
          </div>
        </form>
      ) : canRespond ? (
        <button className="btn-link-danger" onClick={() => setOpen(true)}>{t('detail.respond')}</button>
      ) : null}
      {!isAuthor && (
        <ReportControl
          onReport={(reason, note) => api.reportReview(r.id, { reason, note })}
          authenticated={authenticated}
          login={login}
          buttonLabel={t('report.reviewButton')}
        />
      )}
    </div>
  );
}

// ReportControl is a collapsible report form. onReport(reason, note) performs
// the actual call, so the same control reports either a listing or a review.
function ReportControl({ onReport, authenticated, login, buttonLabel }) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState('inappropriate');
  const [note, setNote] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState(null);

  if (done) return <p className="muted report-done">{t('report.done')}</p>;

  if (!open) {
    return (
      <button className="btn-link-danger" onClick={() => setOpen(true)}>
        ⚑ {buttonLabel || t('report.button')}
      </button>
    );
  }

  async function submit(e) {
    e.preventDefault();
    if (!authenticated) {
      login();
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onReport(reason, note);
      setDone(true);
    } catch (err) {
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="report-form" onSubmit={submit}>
      <strong>{t('report.title')}</strong>
      <label>
        {t('report.reason')}
        <select value={reason} onChange={(e) => setReason(e.target.value)}>
          <option value="spam">{t('report.reason.spam')}</option>
          <option value="inappropriate">{t('report.reason.inappropriate')}</option>
          <option value="scam">{t('report.reason.scam')}</option>
          <option value="inaccurate">{t('report.reason.inaccurate')}</option>
          <option value="other">{t('report.reason.other')}</option>
        </select>
      </label>
      <label>
        {t('report.note')}
        <textarea rows="3" value={note} onChange={(e) => setNote(e.target.value)} />
      </label>
      {error && <p className="error">{error}</p>}
      <div className="report-actions">
        <button className="btn btn-primary" type="submit" disabled={submitting}>
          {submitting ? t('report.submitting') : t('report.submit')}
        </button>
        <button className="btn btn-ghost" type="button" onClick={() => setOpen(false)}>
          {t('common.cancel')}
        </button>
      </div>
    </form>
  );
}
