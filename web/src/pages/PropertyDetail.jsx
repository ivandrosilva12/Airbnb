import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useFavorites } from '../context/FavoritesContext';
import { useT } from '../i18n/I18nContext';
import AvailabilityCalendar from '../components/AvailabilityCalendar';

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
  const [message, setMessage] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.getProperty(id).then(setProperty).catch((e) => setError(e.message));
    api.getReviews(id).then((r) => setReviews(r.items || [])).catch(() => {});
    api.getReviewSummary(id).then(setSummary).catch(() => {});
    api.availability(id).then((a) => setBooked(a.booked || [])).catch(() => {});
  }, [id]);

  async function book(e) {
    e.preventDefault();
    setError(null);
    setMessage(null);
    if (!authenticated) {
      login();
      return;
    }
    if (rangeHitsBooked(form.checkIn, form.checkOut, bookedDaySet(booked))) {
      setError(t('detail.datesUnavailable'));
      return;
    }
    try {
      const booking = await api.createBooking({
        propertyId: id,
        checkIn: form.checkIn,
        checkOut: form.checkOut,
        guests: Number(form.guests),
      });
      setMessage(t('detail.booked', { nights: booking.nights, total: booking.totalPrice.display, status: booking.status }));
      api.availability(id).then((a) => setBooked(a.booked || [])).catch(() => {});
    } catch (e) {
      setError(e.message);
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

  return (
    <div className="container detail">
      <h1>{property.title}</h1>
      <p className="card-meta">
        {property.address.city}, {property.address.country} · {t(`type.${property.type}`)} · {t('detail.upToGuests', { n: property.maxGuests })}
        {summary && summary.count > 0 && ` · ★ ${summary.averageRating.toFixed(1)} (${summary.count})`}
      </p>

      <div className="gallery">
        {property.photos.length === 0 && <div className="card-photo-placeholder">{t('common.noPhoto')}</div>}
        {property.photos.map((ph) => (
          <img key={ph.id} src={ph.url} alt="" />
        ))}
      </div>

      <div className="detail-grid">
        <div>
          <p>{property.description || t('detail.noDescription')}</p>

          <h3>{t('detail.availability')}</h3>
          <AvailabilityCalendar booked={booked} months={2} />

          <h3>{t('detail.cancellation')}</h3>
          <p className="policy-line">{t(`policy.${property.cancellationPolicy}`)}</p>

          <h3>{t('detail.amenities')}</h3>
          <ul className="amenities">
            {property.amenities.length === 0 && <li>{t('detail.noAmenities')}</li>}
            {property.amenities.map((a) => (
              <li key={a}>{a}</li>
            ))}
          </ul>

          <h3>{t('detail.reviews')}</h3>
          {reviews.length === 0 && <p>{t('detail.noReviews')}</p>}
          {reviews.map((r) => (
            <div key={r.id} className="review">
              <strong>★ {r.rating}</strong>
              <p>{r.comment}</p>
            </div>
          ))}
        </div>

        <aside className="booking-box">
          <div className="booking-price">
            <strong>{property.pricePerNight.display}</strong> {t('common.perNight')}
          </div>
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
                {cleaningCents > 0 && <div><span>{t('detail.cleaningFee')}</span><span>{fmt(cleaningCents)}</span></div>}
                <div className="muted">{t('detail.serviceNote')}</div>
                <div className="bd-total"><span>{t('detail.beforeFees')}</span><span>{fmt(subtotalCents - discountCents + cleaningCents)}</span></div>
              </div>
            )}
            <button className="btn btn-primary block" type="submit">
              {authenticated ? t('detail.reserve') : t('detail.signInReserve')}
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
          {error && <p className="error">{error}</p>}
          {!isOwnListing && (
            <ReportControl propertyId={property.id} authenticated={authenticated} login={login} />
          )}
        </aside>
      </div>
    </div>
  );
}

// ReportControl is a collapsible "report this listing" form shown on the
// listing detail page for any non-owner.
function ReportControl({ propertyId, authenticated, login }) {
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
        ⚑ {t('report.button')}
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
      await api.reportListing(propertyId, { reason, note });
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
