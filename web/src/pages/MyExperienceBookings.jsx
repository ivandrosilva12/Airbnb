import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// MyExperienceBookings is the guest's "my experience reservations" page,
// the experiences-side counterpart of MyTrips for properties. Bookings are
// listed in a single table (most-recent first comes from the backend) with
// a cancel button that's only enabled while the booking is still cancellable
// (pending or confirmed AND startAt is in the future).
export default function MyExperienceBookings() {
  const { t } = useT();
  const [bookings, setBookings] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.myExperienceBookings();
      setBookings(res?.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function cancel(id) {
    if (!window.confirm(t('expbookings.confirmCancel'))) return;
    try {
      await api.cancelExperienceBooking(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <div className="container">
      <h1>{t('expbookings.title')}</h1>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : bookings.length === 0 ? (
        <p>{t('expbookings.empty')}</p>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>{t('expbookings.cExpId')}</th>
              <th>{t('expbookings.cStart')}</th>
              <th>{t('expbookings.cGuests')}</th>
              <th>{t('expbookings.cStatus')}</th>
              <th>{t('expbookings.cTotal')}</th>
              <th>{t('expbookings.cActions')}</th>
            </tr>
          </thead>
          <tbody>
            {bookings.map((b) => {
              const future = new Date(b.startAt).getTime() > Date.now();
              const cancellable = future && (b.status === 'pending' || b.status === 'confirmed');
              return (
                <tr key={b.id}>
                  <td>
                    <Link to={`/experiences/${b.experienceId}`}>
                      #{b.experienceId.slice(0, 8)}
                    </Link>
                  </td>
                  <td>{new Date(b.startAt).toLocaleString()}</td>
                  <td>{b.guests}</td>
                  <td>
                    <span className={`badge badge-${b.status}`}>
                      {t(`status.${b.status}`)}
                    </span>
                  </td>
                  <td>{b.pricing?.total?.display || '—'}</td>
                  <td>
                    {cancellable && (
                      <button className="btn btn-ghost btn-sm" onClick={() => cancel(b.id)}>
                        {t('expbookings.cancel')}
                      </button>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}
