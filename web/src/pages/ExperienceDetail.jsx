import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// ExperienceDetail renders one experience: hero photo + thumbnail strip on
// top, then the description, address, duration, max guests, language, and
// per-guest price. The "Book this experience" CTA is a disabled stub — the
// experience-booking flow ships in a future slice, once the per-session
// availability semantics are nailed down.
export default function ExperienceDetail() {
  const { id } = useParams();
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
          <button className="btn btn-ghost block" type="button" disabled>
            {t('experiences.detail.bookSoon')}
          </button>
        </aside>
      </div>
    </div>
  );
}
