import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useT } from '../i18n/I18nContext';

// HostEarnings is the host payouts panel: balances per currency plus the
// earnings ledger (an entry per confirmation credit and cancellation refund).
export default function HostEarnings() {
  const { t } = useT();
  const { isHost } = useAuth();
  const [balances, setBalances] = useState([]);
  const [entries, setEntries] = useState([]);
  const [titles, setTitles] = useState({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    if (!isHost) {
      setLoading(false);
      return;
    }
    (async () => {
      try {
        const [summary, ledger, props] = await Promise.all([
          api.hostEarnings(),
          api.hostEarningEntries(),
          api.myProperties(),
        ]);
        setBalances(summary.balances || []);
        setEntries(ledger.items || []);
        const byId = {};
        for (const p of props.items || []) byId[p.id] = p.title;
        setTitles(byId);
      } catch (e) {
        setError(e.message);
      } finally {
        setLoading(false);
      }
    })();
  }, [isHost]);

  async function exportCsv() {
    try {
      await api.downloadEarningsCsv();
    } catch (e) {
      setError(e.message);
    }
  }

  if (!isHost) return <div className="container"><p>{t('host.becomeText')}</p></div>;

  return (
    <div className="container">
      <div className="row-between">
        <h1>{t('earnings.title')}</h1>
        <div className="actions">
          {entries.length > 0 && (
            <button className="btn btn-ghost" onClick={exportCsv}>{t('earnings.exportCsv')}</button>
          )}
          <Link to="/host" className="btn btn-ghost">{t('host.backDashboard')}</Link>
        </div>
      </div>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : (
        <>
          <h2>{t('earnings.balanceTitle')}</h2>
          {balances.length === 0 ? (
            <p className="muted">{t('earnings.empty')}</p>
          ) : (
            <div className="balance-grid">
              {balances.map((b) => (
                <div key={b.currency} className="balance-card">
                  <div className="balance-currency">{b.currency}</div>
                  <div className="balance-net">{b.net.display}</div>
                  <div className="balance-breakdown">
                    <span>{t('earnings.earned')}: {b.earned.display}</span>
                    <span>{t('earnings.refunded')}: {b.refunded.display}</span>
                  </div>
                </div>
              ))}
            </div>
          )}

          <h2>{t('earnings.ledgerTitle')}</h2>
          {entries.length === 0 ? (
            <p className="muted">{t('earnings.empty')}</p>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>{t('earnings.date')}</th>
                  <th>{t('earnings.type')}</th>
                  <th>{t('earnings.listing')}</th>
                  <th>{t('earnings.amount')}</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((e) => (
                  <tr key={e.id}>
                    <td>{new Date(e.createdAt).toLocaleDateString()}</td>
                    <td><span className={`badge badge-payout-${e.kind}`}>{t(`earnings.kind.${e.kind}`)}</span></td>
                    <td>
                      {titles[e.propertyId]
                        ? <Link to={`/properties/${e.propertyId}`}>{titles[e.propertyId]}</Link>
                        : e.propertyId}
                    </td>
                    <td className={e.kind === 'refund' ? 'amount-debit' : 'amount-credit'}>
                      {e.kind === 'refund' ? '−' : '+'}{e.amount.display}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </div>
  );
}
