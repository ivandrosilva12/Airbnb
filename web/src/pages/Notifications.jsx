import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useNotifications } from '../context/NotificationsContext';
import { useT } from '../i18n/I18nContext';

const ICONS = {
  booking_requested: '📩',
  booking_confirmed: '✅',
  booking_cancelled: '❌',
  booking_modified: '📝',
  saved_search_alert: '🔎',
  message_received: '💬',
  kyc_step_up_required: '🛡️',
  review_submitted: '⭐',
  // S163 — close the "tap dead-end" gap for the 8 notification types
  // that existed but had no client-side onClick wiring. Icons mirror
  // the visual language already in use elsewhere in the product.
  dispute_opened: '⚖️',
  dispute_resolved: '⚖️',
  split_payment_completed: '💳',
  offer_received: '💸',
  offer_declined: '💸',
  offer_withdrawn: '💸',
  cohost_invited: '👥',
  identity_verified: '🛂',
  payment_captured: '💰',
  payment_refunded: '↩️',
};

export default function Notifications() {
  const { items, unread, refresh, markRead, markUnread, markAllRead } = useNotifications();
  const { t } = useT();
  const navigate = useNavigate();

  useEffect(() => {
    refresh();
  }, []);

  function onClick(n) {
    if (!n.read) markRead(n.id);
    if (n.type === 'message_received') navigate('/messages');
    else if (n.type === 'kyc_step_up_required') navigate('/settings');
    else if (n.type === 'review_submitted') navigate('/trips');
    else if (n.type.startsWith('booking_')) navigate('/trips');
    // S163 — deep-links for the previously-dead notification types.
    // RelatedID semantics come from backend/internal/application/
    // notification/subscriber.go. Dispute events carry the dispute
    // UUID; the split_payment_completed event carries the booking UUID
    // (the split itself isn't on the payload), so we route the user
    // back to /trips rather than build a /splits/<bookingId> link that
    // would 404. Offer/cohost/identity types either send the user to a
    // canonical landing page (messages, trips, host, settings) where
    // the next action lives — none of them deep-link by RelatedID
    // because there's no detail page wired for them yet.
    else if ((n.type === 'dispute_opened' || n.type === 'dispute_resolved') && n.relatedId) {
      navigate(`/disputes/${n.relatedId}`);
    } else if (n.type === 'split_payment_completed') {
      navigate('/trips');
    } else if (n.type === 'offer_received') navigate('/messages');
    else if (n.type === 'offer_declined' || n.type === 'offer_withdrawn') navigate('/trips');
    else if (n.type === 'cohost_invited') navigate('/host');
    else if (n.type === 'identity_verified') navigate('/settings');
    else if (n.type === 'payment_captured' || n.type === 'payment_refunded') navigate('/trips');
  }

  return (
    <main className="container" aria-label={t('notif.title')}>
      <div className="row-between">
        <h1>{t('notif.title')}</h1>
        {unread > 0 && (
          <button
            className="btn btn-ghost"
            onClick={markAllRead}
            aria-label={`${t('notif.markAll')} (${unread} unread)`}
          >{t('notif.markAll')}</button>
        )}
      </div>
      {items.length === 0 ? (
        <p>{t('notif.none')}</p>
      ) : (
        <ul className="notif-list" aria-label={t('notif.title')}>
          {items.map((n) => {
            // S163 — pick one accent class per type so the new
            // notification families (dispute / split / offer / cohost /
            // identity / payment) read distinctly in the list without
            // burying the styling logic in the JSX className expression.
            let accent = '';
            if (n.type === 'kyc_step_up_required') accent = 'notif-kyc';
            else if (n.type === 'review_submitted') accent = 'notif-review';
            else if (n.type === 'dispute_opened' || n.type === 'dispute_resolved') accent = 'notif-dispute';
            else if (n.type === 'split_payment_completed') accent = 'notif-split';
            else if (n.type === 'offer_received' || n.type === 'offer_declined' || n.type === 'offer_withdrawn') accent = 'notif-offer';
            else if (n.type === 'cohost_invited') accent = 'notif-cohost';
            else if (n.type === 'identity_verified') accent = 'notif-identity';
            else if (n.type === 'payment_captured' || n.type === 'payment_refunded') accent = 'notif-payment';
            // CTA label key by type. Kept as a small lookup so the JSX
            // below stays a single conditional render instead of a
            // ladder of && expressions.
            let ctaKey = '';
            if (n.type === 'kyc_step_up_required') ctaKey = 'notif.kycStepUp.ctaLabel';
            else if (n.type === 'review_submitted') ctaKey = 'notif.review.ctaLabel';
            else if (n.type === 'dispute_opened' || n.type === 'dispute_resolved') ctaKey = 'notif.dispute.ctaLabel';
            else if (n.type === 'split_payment_completed') ctaKey = 'notif.split.ctaLabel';
            else if (n.type === 'offer_received') ctaKey = 'notif.offer.ctaLabel';
            else if (n.type === 'offer_declined' || n.type === 'offer_withdrawn') ctaKey = 'notif.offer.tripsCtaLabel';
            else if (n.type === 'cohost_invited') ctaKey = 'notif.cohost.ctaLabel';
            else if (n.type === 'identity_verified') ctaKey = 'notif.identity.ctaLabel';
            else if (n.type === 'payment_captured' || n.type === 'payment_refunded') ctaKey = 'notif.payment.ctaLabel';
            return (
            <li
              key={n.id}
              className={`notif-item${n.read ? '' : ' notif-unread'}${accent ? ' ' + accent : ''}`}
              onClick={() => onClick(n)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onClick(n);
                }
              }}
              aria-label={`${n.read ? '' : 'Unread, '}${n.title}: ${n.body}`}
            >
              <span className="notif-icon" aria-hidden="true">{ICONS[n.type] || '🔔'}</span>
              <div className="notif-body">
                <div className="notif-title">{n.title}</div>
                <div className="notif-text">{n.body}</div>
                {ctaKey && (
                  <div className={`notif-cta${n.type === 'review_submitted' ? ' notif-cta-review' : ''}`}>{t(ctaKey)}</div>
                )}
                <div className="notif-time">{n.createdAt ? new Date(n.createdAt).toLocaleString() : ''}</div>
              </div>
              <button
                className="notif-toggle"
                onClick={(e) => { e.stopPropagation(); (n.read ? markUnread : markRead)(n.id); }}
                aria-label={n.read ? t('notif.markUnread') : t('notif.markRead')}
                aria-pressed={!n.read}
              >
                {n.read ? t('notif.markUnread') : t('notif.markRead')}
              </button>
              {!n.read && <span className="notif-dot" aria-hidden="true" />}
            </li>
            );
          })}
        </ul>
      )}
    </main>
  );
}
