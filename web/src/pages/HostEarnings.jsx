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
  const [available, setAvailable] = useState([]);
  const [account, setAccount] = useState({ hasAccount: false, enabled: false });
  const [disbursements, setDisbursements] = useState([]);
  const [entries, setEntries] = useState([]);
  const [titles, setTitles] = useState({});
  const [loading, setLoading] = useState(true);
  const [payingOut, setPayingOut] = useState(null); // currency in flight
  const [onboarding, setOnboarding] = useState(false);
  const [error, setError] = useState(null);

  async function refresh() {
    const [summary, avail, payouts, ledger, props] = await Promise.all([
      api.hostEarnings(),
      api.payoutAvailable(),
      api.listDisbursements(),
      api.hostEarningEntries(),
      api.myProperties(),
    ]);
    setBalances(summary.balances || []);
    setAvailable(avail.available || []);
    setAccount(avail.account || { hasAccount: false, enabled: false });
    setDisbursements(payouts.items || []);
    setEntries(ledger.items || []);
    const byId = {};
    for (const p of props.items || []) byId[p.id] = p.title;
    setTitles(byId);
  }

  useEffect(() => {
    if (!isHost) {
      setLoading(false);
      return;
    }
    // Returning from the provider's onboarding flow: re-check the account state
    // with the gateway, then drop the marker from the URL.
    const returning = new URLSearchParams(window.location.search).get('payout') === 'return';
    const init = returning
      ? api.refreshPayoutAccount().catch(() => {}).then(refresh)
      : refresh();
    init
      .catch((e) => setError(e.message))
      .finally(() => {
        setLoading(false);
        if (returning) window.history.replaceState({}, '', '/host/earnings');
      });
  }, [isHost]);

  async function startOnboarding() {
    setError(null);
    setOnboarding(true);
    try {
      const returnUrl = `${window.location.origin}/host/earnings?payout=return`;
      const { url } = await api.onboardPayouts({ returnUrl, refreshUrl: returnUrl });
      window.location.href = url;
    } catch (e) {
      setError(e.message);
      setOnboarding(false);
    }
  }

  async function requestPayout(currency) {
    setError(null);
    setPayingOut(currency);
    try {
      await api.requestPayout(currency);
      await refresh();
    } catch (e) {
      setError(e.message);
    } finally {
      setPayingOut(null);
    }
  }

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

          <h2>{t('earnings.payoutsTitle')}</h2>
          {!account.enabled ? (
            <div className="payout-onboard">
              <p className="muted">{t('earnings.onboardHint')}</p>
              <button className="btn btn-primary" disabled={onboarding} onClick={startOnboarding}>
                {onboarding ? t('earnings.onboardingBusy') : t('earnings.onboardCta')}
              </button>
            </div>
          ) : available.length === 0 ? (
            <p className="muted">{t('earnings.noAvailable')}</p>
          ) : (
            <div className="balance-grid">
              {available.map((a) => (
                <div key={a.currency} className="balance-card">
                  <div className="balance-currency">{a.currency}</div>
                  <div className="balance-net">{a.available.display}</div>
                  <div className="balance-breakdown">
                    <span>{t('earnings.availableLabel')}</span>
                  </div>
                  <button
                    className="btn btn-primary block"
                    disabled={a.available.amountCents <= 0 || payingOut === a.currency}
                    onClick={() => requestPayout(a.currency)}
                  >
                    {payingOut === a.currency ? t('earnings.payingOut') : t('earnings.requestPayout')}
                  </button>
                </div>
              ))}
            </div>
          )}

          {disbursements.length > 0 && (
            <table className="table">
              <thead>
                <tr>
                  <th>{t('earnings.date')}</th>
                  <th>{t('earnings.amount')}</th>
                  <th>{t('earnings.payoutStatus')}</th>
                  <th>{t('earnings.payoutRef')}</th>
                </tr>
              </thead>
              <tbody>
                {disbursements.map((d) => (
                  <tr key={d.id}>
                    <td>{new Date(d.createdAt).toLocaleDateString()}</td>
                    <td>{d.amount.display}</td>
                    <td><span className={`badge badge-payout-${d.status}`}>{t(`earnings.payoutState.${d.status}`)}</span></td>
                    <td className="muted">{d.gatewayRef || d.failure || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
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
