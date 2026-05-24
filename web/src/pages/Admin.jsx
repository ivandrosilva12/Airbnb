import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// Admin is the platform moderation console. It currently surfaces the KYC
// identity-verification review queue; listing-report moderation is added here
// alongside it.
export default function Admin() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminListVerifications();
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function approve(id) {
    setError(null);
    try {
      await api.adminApproveVerification(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function reject(id) {
    const reason = window.prompt(t('admin.rejectPrompt'));
    if (!reason) return;
    setError(null);
    try {
      await api.adminRejectVerification(id, reason);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <div className="container">
      <h1>{t('admin.title')}</h1>
      <h2>{t('admin.verifTitle')}</h2>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.verifEmpty')}</p>
      ) : (
        <table className="admin-table">
          <thead>
            <tr>
              <th>{t('admin.legalName')}</th>
              <th>{t('admin.document')}</th>
              <th>{t('admin.submitted')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((v) => (
              <tr key={v.id}>
                <td>{v.legalName}</td>
                <td>{t(`verify.doc.${v.documentType}`)}</td>
                <td>{new Date(v.createdAt).toLocaleDateString()}</td>
                <td className="admin-actions">
                  <button className="btn btn-primary" onClick={() => approve(v.id)}>
                    {t('admin.approve')}
                  </button>
                  <button className="btn btn-ghost" onClick={() => reject(v.id)}>
                    {t('admin.reject')}
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
