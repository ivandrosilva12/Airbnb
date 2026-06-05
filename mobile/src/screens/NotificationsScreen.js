import { useEffect, useState, useCallback } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useNavigation } from '@react-navigation/native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { useT } from '../i18n/I18nContext';

// TODO(i18n): translate — wrap hardcoded labels via useT() from ../i18n/I18nContext.
// Key namespace mirrors the web (notif.*).

export default function NotificationsScreen() {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const navigation = useNavigation();
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [unread, setUnread] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.listNotifications();
      setItems(res.items || []);
      setUnread(res.unread || 0);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    if (authenticated) load();
    else setLoading(false);
  }, [authenticated]);

  async function markRead(id) {
    setItems((prev) => prev.map((n) => (n.id === id ? { ...n, read: true } : n)));
    setUnread((u) => Math.max(0, u - 1));
    try {
      await api.markNotificationRead(id);
    } catch {
      load();
    }
  }

  async function markUnread(id) {
    setItems((prev) => prev.map((n) => (n.id === id ? { ...n, read: false } : n)));
    setUnread((u) => u + 1);
    try {
      await api.markNotificationUnread(id);
    } catch {
      load();
    }
  }

  async function markAll() {
    setItems((prev) => prev.map((n) => ({ ...n, read: true })));
    setUnread(0);
    try {
      await api.markAllNotificationsRead();
    } catch {
      load();
    }
  }

  // S145 — tapping a notification both clears its unread flag and, for the
  // typed rows we know how to route, navigates to the relevant screen. The
  // kyc_step_up_required type is emitted by the backend (S140) when a guest
  // attempts a high-value booking without a verified identity, so the row
  // routes straight to the Verification flow — same UX contract as the web.
  //
  // S150 — review_submitted (emitted by S148 once a guest leaves a review)
  // routes to the Trips screen, where the reviewed stay is visible and the
  // guest can read the public review on the listing. Matches web S149: no
  // dedicated "my reviews" screen exists yet, so Trips is the closest
  // destination that surfaces the reviewed booking.
  //
  // S164 — wires deep-links for every remaining backend-emitted notification
  // type so taps don't dead-end. The branches are grouped by destination:
  //   - Trips:                booking_*, offer_declined/withdrawn,
  //                           payment_captured/refunded
  //   - Messages/Conversation: message_received, offer_received
  //   - Dispute:              dispute_opened/resolved (relatedID == disputeID)
  //   - Splits:               split_payment_completed
  //   - MyExperienceBookings: experience_booking_*
  //   - CohostInvitation:     cohost_invited (relatedID == invitationID)
  //   - Verification:         kyc_step_up_required, identity_verified
  // Unknown types fall through silently (consistent with prior behaviour).
  function onRowPress(n) {
    if (!n.read) markRead(n.id);
    const nt = n.type || '';
    // Identity flows route to the same Verification screen so the user can
    // see their current status (pending review, verified, rejected, etc.).
    if (nt === 'kyc_step_up_required' || nt === 'identity_verified') {
      navigation.navigate('Verification');
      return;
    }
    if (nt === 'review_submitted') {
      navigation.navigate('Trips');
      return;
    }
    // booking_* (requested/confirmed/cancelled/modified/completed) and
    // payment_* (captured/refunded) all surface on Trips — the screen lists
    // every booking the user has + its current payment state.
    if (nt.startsWith('booking_') || nt === 'payment_captured' || nt === 'payment_refunded') {
      navigation.navigate('Trips');
      return;
    }
    // Message + offer_received are conversation-bound. Prefer deep-linking
    // straight into the thread if the backend supplied a conversationID via
    // relatedID; otherwise fall back to the inbox.
    if (nt === 'message_received' || nt === 'offer_received') {
      if (n.relatedID) {
        navigation.navigate('Conversation', { conversationID: n.relatedID });
      } else {
        navigation.navigate('Messages');
      }
      return;
    }
    // Offer lifecycle outside the conversation (declined / withdrawn) surfaces
    // on Trips — the offer was tied to a booking the guest can see there.
    if (nt === 'offer_declined' || nt === 'offer_withdrawn') {
      navigation.navigate('Trips');
      return;
    }
    if (nt === 'dispute_opened' || nt === 'dispute_resolved') {
      // DisputeScreen expects a disputeID param; relatedID carries it.
      navigation.navigate('Dispute', { disputeID: n.relatedID });
      return;
    }
    if (nt === 'split_payment_completed') {
      navigation.navigate('Splits');
      return;
    }
    if (nt.startsWith('experience_booking_')) {
      navigation.navigate('MyExperienceBookings');
      return;
    }
    if (nt === 'cohost_invited') {
      navigation.navigate('CohostInvitation', { invitationID: n.relatedID });
      return;
    }
  }

  // S164 — icon + chip category for every known notification type. Mirrors
  // the web ICONS map (S163) but expanded to cover every backend emitter.
  // categoryOf() bundles three render concerns: the emoji prefix, the i18n
  // namespace used for the chip+CTA strings, and the visual style key
  // (rendered via styles[`${key}TagText`]/`${key}TagCta`).
  function categoryOf(type) {
    const nt = type || '';
    if (nt === 'kyc_step_up_required') {
      return { icon: '🛡️', key: 'kycStepUp', style: 'action' };
    }
    if (nt === 'identity_verified') {
      return { icon: '🛂', key: 'identity', style: 'identity' };
    }
    if (nt === 'review_submitted') {
      return { icon: '⭐', key: 'review', style: 'review' };
    }
    if (nt === 'dispute_opened' || nt === 'dispute_resolved') {
      return { icon: '⚖️', key: 'dispute', style: 'dispute' };
    }
    if (nt === 'split_payment_completed') {
      return { icon: '💳', key: 'split', style: 'split' };
    }
    if (nt === 'offer_received' || nt === 'offer_declined' || nt === 'offer_withdrawn') {
      return { icon: '💸', key: 'offer', style: 'offer' };
    }
    if (nt === 'cohost_invited') {
      return { icon: '👥', key: 'cohost', style: 'cohost' };
    }
    if (nt === 'payment_captured') {
      return { icon: '💰', key: 'payment', style: 'paymentIn' };
    }
    if (nt === 'payment_refunded') {
      return { icon: '↩️', key: 'payment', style: 'paymentOut' };
    }
    if (nt.startsWith('experience_booking_')) {
      return { icon: '🌟', key: 'experienceBooking', style: 'experience' };
    }
    if (nt === 'message_received') {
      return { icon: '💬', key: null, style: null };
    }
    // booking_* — pick the prefix per lifecycle stage so the row reads at a
    // glance. No chip rendered: Trips already shows the booking in context.
    if (nt === 'booking_requested') return { icon: '📩', key: null, style: null };
    if (nt === 'booking_confirmed') return { icon: '✅', key: null, style: null };
    if (nt === 'booking_cancelled') return { icon: '❌', key: null, style: null };
    if (nt === 'booking_modified') return { icon: '📝', key: null, style: null };
    if (nt === 'booking_completed') return { icon: '🏁', key: null, style: null };
    return { icon: '', key: null, style: null };
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to see your notifications.</Text>
        <Pressable style={styles.btn} onPress={login}><Text style={styles.btnText}>Sign in</Text></Pressable>
      </View>
    );
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <View style={styles.container}>
      {error && <Text style={styles.error}>{error}</Text>}
      {unread > 0 && (
        <Pressable style={styles.markAll} onPress={markAll}>
          <Text style={styles.markAllText}>Mark all read ({unread})</Text>
        </Pressable>
      )}
      <FlatList
        data={items}
        keyExtractor={(i) => i.id}
        ListEmptyComponent={<Text style={styles.empty}>No notifications.</Text>}
        renderItem={({ item }) => {
          // S164 — categoryOf resolves icon + chip styling per type. Rows
          // without a chip key (e.g. booking_*, message_received) still get
          // the emoji prefix to differentiate them at a glance.
          const cat = categoryOf(item.type);
          const tagTextStyle = cat.style ? styles[`${cat.style}TagText`] : null;
          const tagCtaStyle = cat.style ? styles[`${cat.style}TagCta`] : null;
          const tagLabel = cat.key ? t(`notif.${cat.key}.tag`) : '';
          const ctaLabel = cat.key ? t(`notif.${cat.key}.ctaLabel`) : '';
          return (
            <Pressable style={[styles.row, !item.read && styles.unreadRow]} onPress={() => onRowPress(item)}>
              {!item.read && <View style={styles.dot} />}
              <View style={{ flex: 1 }}>
                <Text style={styles.title}>
                  {/* S145/S150/S164 — emoji prefix per category. Shield = KYC,
                      star = review, ⚖️ = dispute, 💳 = split, 💸 = offer,
                      👥 = cohost, 🛂 = identity_verified, 💰/↩️ = payment,
                      🌟 = experience_booking_*, 📩/✅/❌/📝/🏁 = booking_*. */}
                  {cat.icon ? `${cat.icon} ` : ''}{item.title}
                </Text>
                <Text style={styles.meta}>{item.body}</Text>
                {cat.key && tagTextStyle && tagCtaStyle && (
                  // Chip + CTA share a row. Each style key has a paired
                  // ${style}TagText (chip background) and ${style}TagCta
                  // (arrow link) entry in the StyleSheet below.
                  <View style={styles.actionTag}>
                    <Text style={tagTextStyle}>{tagLabel}</Text>
                    <Text style={tagCtaStyle}>{ctaLabel} →</Text>
                  </View>
                )}
              </View>
              <Pressable onPress={() => (item.read ? markUnread(item.id) : markRead(item.id))} hitSlop={8}>
                <Text style={styles.toggle}>{item.read ? 'Mark unread' : 'Mark read'}</Text>
              </Pressable>
            </Pressable>
          );
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 12, borderBottomWidth: 1, borderColor: '#eee' },
  unreadRow: { backgroundColor: '#fff5f7' },
  dot: { width: 8, height: 8, borderRadius: 4, backgroundColor: '#ff385c' },
  title: { fontWeight: '600' },
  meta: { color: '#717171', marginTop: 2 },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
  markAll: { alignSelf: 'flex-end', paddingVertical: 8 },
  markAllText: { color: '#ff385c', fontWeight: '700' },
  toggle: { color: '#717171', fontSize: 12, textDecorationLine: 'underline' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
  // S145 — action-required tag sits under the body and visually nudges the
  // user to tap the row. The chip + CTA share a row to keep the layout dense.
  actionTag: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 6 },
  actionTagText: {
    color: '#b54708',
    backgroundColor: '#fff6ed',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  actionTagCta: { color: '#ff385c', fontSize: 12, fontWeight: '600' },
  // S150 — softer gold palette for the review_submitted chip. Same shape as
  // actionTagText, but the colours signal "good news to read" instead of
  // "you must act now".
  reviewTagText: {
    color: '#7a5b00',
    backgroundColor: '#fff8db',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  reviewTagCta: { color: '#a07900', fontSize: 12, fontWeight: '600' },
  // S164 — palette per new category. Each pair keeps the chip+CTA shape
  // identical (so the visual rhythm with kyc/review is preserved) and just
  // varies the colours to signal severity:
  //   dispute     — slate/gray-blue (sober, formal)
  //   split       — teal (financial, neutral)
  //   offer       — purple (offer/promo)
  //   cohost      — indigo (collaboration)
  //   identity    — green (success)
  //   paymentIn   — green (money in)
  //   paymentOut  — amber (money out, refund)
  //   experience  — pink (matches the experiences brand accent)
  disputeTagText: {
    color: '#334155',
    backgroundColor: '#e2e8f0',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  disputeTagCta: { color: '#475569', fontSize: 12, fontWeight: '600' },
  splitTagText: {
    color: '#0f766e',
    backgroundColor: '#ccfbf1',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  splitTagCta: { color: '#0d9488', fontSize: 12, fontWeight: '600' },
  offerTagText: {
    color: '#6b21a8',
    backgroundColor: '#f3e8ff',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  offerTagCta: { color: '#7c3aed', fontSize: 12, fontWeight: '600' },
  cohostTagText: {
    color: '#3730a3',
    backgroundColor: '#e0e7ff',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  cohostTagCta: { color: '#4f46e5', fontSize: 12, fontWeight: '600' },
  identityTagText: {
    color: '#15803d',
    backgroundColor: '#dcfce7',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  identityTagCta: { color: '#16a34a', fontSize: 12, fontWeight: '600' },
  paymentInTagText: {
    color: '#166534',
    backgroundColor: '#dcfce7',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  paymentInTagCta: { color: '#15803d', fontSize: 12, fontWeight: '600' },
  paymentOutTagText: {
    color: '#92400e',
    backgroundColor: '#fef3c7',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  paymentOutTagCta: { color: '#b45309', fontSize: 12, fontWeight: '600' },
  experienceTagText: {
    color: '#9d174d',
    backgroundColor: '#fce7f3',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  experienceTagCta: { color: '#be185d', fontSize: 12, fontWeight: '600' },
});
