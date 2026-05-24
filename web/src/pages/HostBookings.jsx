import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

function GuestReviewForm({ bookingId, onDone, onError }) {
  const { t } = useT();
  const [rating, setRating] = useState(5);
  const [comment, setComment] = useState('');
  const [submitting, setSubmitting] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setSubmitting(true);
    try {
      await api.createGuestReview({ bookingId, rating: Number(rating), comment });
      onDone();
    } catch (err) {
      onError(err.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="inline-review" onSubmit={submit}>
      <select value={rating} onChange={(e) => setRating(e.target.value)}>
        {[5, 4, 3, 2, 1].map((n) => (
          <option key={n} value={n}>{'★'.repeat(n)}</option>
        ))}
      </select>
      <input placeholder={t('host.howWasGuest')} value={comment} onChange={(e) => setComment(e.target.value)} />
      <button className="btn btn-primary" type="submit" disabled={submitting}>{submitting ? '…' : t('common.submit')}</button>
    </form>
  );
}

export default function HostBookings() {
  const { t } = useT();
  const { id } = useParams();
  const [property, setProperty] = useState(null);
  const [bookings, setBookings] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [reviewing, setReviewing] = useState(null);
  const [reviewed, setReviewed] = useState({});
  const [blocks, setBlocks] = useState([]);
  const [blockForm, setBlockForm] = useState({ from: '', to: '', reason: '' });
  const [icalText, setIcalText] = useState('');
  const [icalMsg, setIcalMsg] = useState(null);

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const [prop, res, blocksRes] = await Promise.all([
        api.getProperty(id),
        api.propertyBookings(id),
        api.listBlocks(id),
      ]);
      setProperty(prop);
      setBookings(res.items || []);
      setBlocks(blocksRes.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, [id]);

  async function act(fn, bookingId) {
    setError(null);
    try {
      await fn(bookingId);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function addBlock(e) {
    e.preventDefault();
    setError(null);
    try {
      await api.createBlock(id, blockForm);
      setBlockForm({ from: '', to: '', reason: '' });
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function removeBlock(blockId) {
    setError(null);
    try {
      await api.deleteBlock(blockId);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function importCalendar(e) {
    e.preventDefault();
    setError(null);
    setIcalMsg(null);
    try {
      const res = await api.importCalendar(id, icalText);
      setIcalText('');
      setIcalMsg(t('ical.imported', { imported: res.imported, found: res.found }));
      load();
    } catch (err) {
      setError(err.message);
    }
  }

  return (
    <div className="container">
      <p><Link to="/host">{t('host.backDashboard')}</Link></p>
      <h1>{t('host.bookings')}{property ? ` · ${property.title}` : ''}</h1>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : bookings.length === 0 ? (
        <p>{t('host.noBookings')}</p>
      ) : (
        <table className="table">
          <thead>
            <tr><th>{t('trips.dates')}</th><th>{t('common.guests')}</th><th>{t('trips.total')}</th><th>{t('trips.status')}</th><th></th></tr>
          </thead>
          <tbody>
            {bookings.map((b) => (
              <tr key={b.id}>
                <td>{b.checkIn} → {b.checkOut} ({b.nights}n)</td>
                <td>{b.guests}</td>
                <td>{b.totalPrice.display}</td>
                <td><span className={`badge badge-${b.status}`}>{b.status}</span></td>
                <td className="actions">
                  {b.status === 'pending' && (
                    <button className="btn btn-ghost" onClick={() => act(api.confirmBooking, b.id)}>{t('host.confirm')}</button>
                  )}
                  {b.status === 'confirmed' && (
                    <button className="btn btn-ghost" onClick={() => act(api.completeBooking, b.id)}>{t('host.markCompleted')}</button>
                  )}
                  {(b.status === 'pending' || b.status === 'confirmed') && (
                    <button className="btn btn-ghost" onClick={() => act(api.cancelBooking, b.id)}>{t('common.cancel')}</button>
                  )}
                  {b.status === 'completed' && !reviewed[b.id] && reviewing !== b.id && (
                    <button className="btn btn-ghost" onClick={() => { setError(null); setReviewing(b.id); }}>{t('host.reviewGuest')}</button>
                  )}
                  {reviewed[b.id] && <span className="success">{t('host.guestReviewed')}</span>}
                  {reviewing === b.id && (
                    <GuestReviewForm
                      bookingId={b.id}
                      onDone={() => { setReviewing(null); setReviewed((r) => ({ ...r, [b.id]: true })); }}
                      onError={setError}
                    />
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {!loading && (
        <section className="blocks-section">
          <h2>{t('host.blockedDates')}</h2>
          <form className="block-form" onSubmit={addBlock}>
            <label>{t('host.blockFrom')}
              <input type="date" required value={blockForm.from} onChange={(e) => setBlockForm({ ...blockForm, from: e.target.value })} />
            </label>
            <label>{t('host.blockTo')}
              <input type="date" required value={blockForm.to} onChange={(e) => setBlockForm({ ...blockForm, to: e.target.value })} />
            </label>
            <input
              placeholder={t('host.blockReason')}
              value={blockForm.reason}
              onChange={(e) => setBlockForm({ ...blockForm, reason: e.target.value })}
            />
            <button className="btn btn-primary" type="submit">{t('host.addBlock')}</button>
          </form>
          {blocks.length === 0 ? (
            <p className="muted-text">{t('host.noBlocks')}</p>
          ) : (
            <ul className="block-list">
              {blocks.map((b) => (
                <li key={b.id}>
                  <span>{b.checkIn} → {b.checkOut}{b.reason ? ` · ${b.reason}` : ''}</span>
                  <button className="link-btn" onClick={() => removeBlock(b.id)}>{t('host.unblock')}</button>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {!loading && (
        <section className="blocks-section">
          <h2>{t('ical.title')}</h2>
          <p className="muted-text">{t('ical.exportHint')}</p>
          <code className="ical-url">{api.calendarFeedUrl(id)}</code>

          <form className="ical-import" onSubmit={importCalendar}>
            <label>{t('ical.importLabel')}
              <textarea
                rows={4}
                placeholder="BEGIN:VCALENDAR…"
                value={icalText}
                onChange={(e) => setIcalText(e.target.value)}
              />
            </label>
            <button className="btn btn-primary" type="submit" disabled={!icalText.trim()}>{t('ical.importBtn')}</button>
          </form>
          {icalMsg && <p className="success">{icalMsg}</p>}
        </section>
      )}
    </div>
  );
}
