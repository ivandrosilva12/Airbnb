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
  function onRowPress(n) {
    if (!n.read) markRead(n.id);
    if (n.type === 'kyc_step_up_required') {
      navigation.navigate('Verification');
    } else if (n.type === 'review_submitted') {
      navigation.navigate('Trips');
    }
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
          const isStepUp = item.type === 'kyc_step_up_required';
          const isReview = item.type === 'review_submitted';
          return (
            <Pressable style={[styles.row, !item.read && styles.unreadRow]} onPress={() => onRowPress(item)}>
              {!item.read && <View style={styles.dot} />}
              <View style={{ flex: 1 }}>
                <Text style={styles.title}>
                  {/* S145 — shield prefix flags the row as a security/identity
                      action so it stands out even when already read.
                      S150 — star prefix flags review_submitted in a softer,
                      celebratory tone (a new review is good news, not a chore). */}
                  {isStepUp ? '🛡️ ' : ''}{isReview ? '⭐ ' : ''}{item.title}
                </Text>
                <Text style={styles.meta}>{item.body}</Text>
                {isStepUp && (
                  <View style={styles.actionTag}>
                    <Text style={styles.actionTagText}>{t('notif.kycStepUp.tag')}</Text>
                    <Text style={styles.actionTagCta}>{t('notif.kycStepUp.ctaLabel')} →</Text>
                  </View>
                )}
                {isReview && (
                  // S150 — same chip+CTA shape as the KYC arm, but rendered in
                  // gold/yellow to keep the tone informational rather than
                  // urgent. Mirrors web S149's softer treatment.
                  <View style={styles.actionTag}>
                    <Text style={styles.reviewTagText}>{t('notif.review.tag')}</Text>
                    <Text style={styles.reviewTagCta}>{t('notif.review.ctaLabel')} →</Text>
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
});
