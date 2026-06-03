import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// HostExperiences renders the host's experience catalogue (S71 BC) with the
// usual draft/published/suspended lifecycle controls. It mirrors the layout of
// HostDashboard's properties table so hosts don't have to relearn anything.
export default function HostExperiences() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  async function load() {
    setLoading(true);
    try {
      const res = await api.hostListExperiences();
      setItems(res?.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function run(fn) {
    setError(null);
    try {
      await fn();
      await load();
    } catch (e) {
      setError(e.message);
    }
  }

  function publish(id) {
    run(() => api.publishExperience(id));
  }
  function suspend(id) {
    run(() => api.suspendExperience(id));
  }
  function remove(id) {
    if (!confirm(t('host.experiences.action.confirmDelete'))) return;
    run(() => api.deleteExperience(id));
  }

  return (
    <div className="container">
      <Link to="/host" className="muted">{t('host.backDashboard')}</Link>
      <div className="row-between">
        <h1>{t('host.experiences.title')}</h1>
        <Link to="/host/experiences/new" className="btn btn-primary">{t('host.experiences.new')}</Link>
      </div>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p>{t('host.experiences.empty')}</p>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>{t('host.experiences.col.title')}</th>
              <th>{t('host.experiences.col.category')}</th>
              <th>{t('host.experiences.col.city')}</th>
              <th>{t('host.experiences.col.status')}</th>
              <th>{t('host.experiences.col.actions')}</th>
            </tr>
          </thead>
          <tbody>
            {items.map((x) => (
              <tr key={x.id}>
                <td>{x.title}</td>
                <td>{x.category}</td>
                <td>{x.city}</td>
                <td>
                  <span className={`badge badge-${x.status}`}>
                    {t(`host.experiences.status.${x.status}`)}
                  </span>
                </td>
                <td className="actions">
                  <Link className="btn btn-ghost" to={`/host/experiences/${x.id}/edit`}>
                    {t('host.experiences.action.edit')}
                  </Link>
                  {x.status === 'draft' && (
                    <button className="btn btn-ghost" onClick={() => publish(x.id)}>
                      {t('host.experiences.action.publish')}
                    </button>
                  )}
                  {x.status === 'published' && (
                    <button className="btn btn-ghost" onClick={() => suspend(x.id)}>
                      {t('host.experiences.action.suspend')}
                    </button>
                  )}
                  <button className="btn btn-ghost" onClick={() => remove(x.id)}>
                    {t('host.experiences.action.delete')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
