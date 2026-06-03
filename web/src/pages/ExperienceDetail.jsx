import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useT } from '../i18n/I18nContext';

// Service-fee rate the backend uses for experience bookings (see
// backend/internal/domain/experiencebooking/pricing.go). Mirrored here only
// for the live total preview — the authoritative number is computed
// server-side and echoed back in the response pricing block.
const SERVICE_FEE_RATE = 0.10;

// ExperienceDetail renders one experience: hero photo + thumbnail strip on
// top, then the description, address, duration, max guests, language, and
// per-guest price. The "Book this experience" panel now hosts a real booking
// form (S83) that POSTs to the S80 ExperienceBooking BC; the form is auth-
// gated, surfaces a price preview, and translates the structured error codes
// (conflict, validation) into actionable copy.
export default function ExperienceDetail() {
  const { id } = useParams();
  const { authenticated, login } = useAuth();
  const { t } = useT();
  const [experience, setExperience] = useState(null);
  const [activePhoto, setActivePhoto] = useState(0);
  const [error, setError] = useState(null);

  useEffect(() => {
    api
      .getExperience(id)
      .then((e) => {
        setExperience(e);
        setActivePhoto(0);
      })
      .catch((e) => setError(e.message));
  }, [id]);

  if (error && !experience) return <div className="container"><p className="error">{error}</p></div>;
  if (!experience) return <div className="container"><p>{t('common.loading')}</p></div>;

  const photos = experience.photos || [];
  const hero = photos[activePhoto];
  const isPublished = experience.status === 'published';

  return (
    <div className="container detail">
      <h1>{experience.title}</h1>
      <p className="card-meta">
        {t(`experiences.category.${experience.category}`)} ·{' '}
        {experience.address.city}, {experience.address.country}
        {!isPublished && (
          <>
            {' · '}
            <span className="superhost-badge">{t(`experiences.status.${experience.status}`)}</span>
          </>
        )}
      </p>
      <p className="muted">{t('experiences.detail.hostedBy', { id: experience.hostId })}</p>

      <div className="gallery">
        {photos.length === 0 && <div className="card-photo-placeholder">{t('common.noPhoto')}</div>}
        {hero && (
          <img
            src={hero.url}
            alt={t('a11y.photoOf', { title: experience.title, n: activePhoto + 1 })}
          />
        )}
      </div>
      {photos.length > 1 && (
        <div className="experience-thumbs">
          {photos.map((p, i) => (
            <button
              key={p.id}
              type="button"
              className={`experience-thumb${i === activePhoto ? ' on' : ''}`}
              onClick={() => setActivePhoto(i)}
              aria-label={t('a11y.photoOf', { title: experience.title, n: i + 1 })}
              aria-pressed={i === activePhoto}
            >
              <img src={p.url} alt="" />
            </button>
          ))}
        </div>
      )}

      <div className="detail-grid">
        <div>
          {experience.description ? (
            <p style={{ whiteSpace: 'pre-line' }}>{experience.description}</p>
          ) : (
            <p>{t('detail.noDescription')}</p>
          )}

          <ul className="amenities">
            <li><strong>{t('experiences.detail.duration')}:</strong> {t('experiences.card.duration', { n: experience.durationMinutes })}</li>
            <li><strong>{t('experiences.detail.maxGuests')}:</strong> {experience.maxGuests}</li>
            <li><strong>{t('experiences.detail.language')}:</strong> {experience.language}</li>
          </ul>
        </div>

        <aside className="booking-box">
          <div className="booking-price">
            <strong>{experience.pricePerGuest.display}</strong> {t('experiences.card.perGuest')}
          </div>
          {isPublished ? (
            <BookingForm experience={experience} authenticated={authenticated} login={login} />
          ) : (
            <button className="btn btn-ghost block" type="button" disabled>
              {t('experiences.detail.bookSoon')}
            </button>
          )}
        </aside>
      </div>
    </div>
  );
}

// BookingForm captures the datetime + guest count and posts to the ExperienceBooking
// BC. On 201 we replace the form with a confirmation card; 409 ("session_taken")
// is the only error worth a custom message — everything else surfaces the
// backend's error string verbatim. The total preview mirrors the backend's
// 10% service-fee formula so the guest sees the same number before submit.
function BookingForm({ experience, authenticated, login }) {
  const { t } = useT();
  const [startAt, setStartAt] = useState('');
  const [guests, setGuests] = useState(1);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);
  const [booking, setBooking] = useState(null);

  // Live total preview — pricePerGuest.amountCents × guests × (1 + fee).
  const priceCents = experience.pricePerGuest?.amountCents ?? 0;
  const currency = experience.pricePerGuest?.currency || 'EUR';
  const guestsN = Math.max(1, Number(guests) || 1);
  const subtotalCents = priceCents * guestsN;
  const feeCents = Math.floor(subtotalCents * SERVICE_FEE_RATE);
  const totalCents = subtotalCents + feeCents;
  const fmt = (cents) => `${(cents / 100).toFixed(2)} ${currency}`;

  async function submit(e) {
    e.preventDefault();
    if (!authenticated) {
      login();
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      // The browser <input type="datetime-local"> yields "2026-07-01T18:30"
      // (no timezone). Promote to ISO-8601 so the backend's RFC3339 parser
      // accepts it — we treat the input as the visitor's local wall clock.
      const iso = new Date(startAt).toISOString();
      const res = await api.createExperienceBooking(experience.id, { startAt: iso, guests: guestsN });
      setBooking(res);
    } catch (err) {
      if (err.status === 401) {
        login();
        return;
      }
      if (err.status === 409) {
        setError(t('experiences.book.conflict'));
      } else {
        setError(err.message);
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (booking) {
    return (
      <div className="success" role="status" style={{ display: 'block' }}>
        <p>{t('experiences.book.success', { status: booking.status })}</p>
        <p className="muted-text" style={{ fontSize: '.82rem' }}>#{booking.id.slice(0, 8)}</p>
        <Link to="/experience-bookings/me" className="btn btn-ghost block" style={{ marginTop: '.5rem' }}>
          {t('experiences.book.viewMine')}
        </Link>
      </div>
    );
  }

  if (!authenticated) {
    return (
      <div>
        <p className="muted-text">{t('experiences.book.signInPrompt')}</p>
        <button type="button" className="btn btn-primary block" onClick={login}>
          {t('experiences.book.signIn')}
        </button>
      </div>
    );
  }

  return (
    <form onSubmit={submit}>
      <h3 style={{ marginTop: 0 }}>{t('experiences.book.title')}</h3>
      <label className="form-row">
        <span>{t('experiences.book.startAt')}</span>
        <input
          type="datetime-local"
          required
          value={startAt}
          onChange={(e) => setStartAt(e.target.value)}
        />
      </label>
      <label className="form-row">
        <span>{t('experiences.book.guests')}</span>
        <input
          type="number"
          required
          min={1}
          max={experience.maxGuests}
          value={guests}
          onChange={(e) => setGuests(e.target.value)}
        />
      </label>
      <div className="muted-text" style={{ fontSize: '.85rem', margin: '.5rem 0' }}>
        <div>{t('experiences.book.totalPreview')}</div>
        <strong>{fmt(totalCents)}</strong>
      </div>
      {error && <p className="error">{error}</p>}
      <button className="btn btn-primary block" type="submit" disabled={submitting || !startAt}>
        {submitting ? '…' : t('experiences.book.submit')}
      </button>
    </form>
  );
}
