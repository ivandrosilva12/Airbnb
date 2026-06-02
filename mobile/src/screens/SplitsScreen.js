import { useCallback, useEffect, useState } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator, Alert } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

// SplitsScreen lists every split-payment plan the caller participates in
// (either as organizer or as one of the named payers). Tapping a row expands
// it into the per-share breakdown with an "Authorise my share" action on the
// caller's own row while the plan is still pending. The organizer also gets a
// "Cancel split" CTA.
//
// Trust-mode parity with the web SplitPay page: authorising does NOT call a
// real card gateway — the payer's tap IS the commitment. When every share is
// paid the backend confirms the booking automatically.
export default function SplitsScreen({ navigation, route }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [items, setItems] = useState([]);
  const [expanded, setExpanded] = useState({});
  const [myEmail, setMyEmail] = useState('');
  const [myUserId, setMyUserId] = useState('');
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState(null);
  const [error, setError] = useState(null);

  // If we landed from a "Pay your share" deep-link the route carries a splitId
  // — pre-expand that row so the payer sees the authorise CTA immediately.
  const preselect = route?.params?.splitId || null;

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [splitsRes, meRes] = await Promise.all([
        api.mySplits(),
        api.me().catch(() => null),
      ]);
      const list = (splitsRes && splitsRes.items) || [];
      setItems(list);
      setMyEmail((meRes?.email || '').toLowerCase());
      setMyUserId(meRes?.id || '');
      if (preselect) setExpanded((m) => ({ ...m, [preselect]: true }));
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api, preselect]);

  useEffect(() => {
    if (authenticated) load();
    else setLoading(false);
  }, [authenticated, load]);

  function toggle(id) {
    setExpanded((m) => ({ ...m, [id]: !m[id] }));
  }

  function fmt(cents, currency) {
    return `${(cents / 100).toFixed(2)} ${currency}`;
  }

  async function authorize(split) {
    const mine = split.shares.find((s) => (s.payerEmail || '').toLowerCase() === myEmail);
    if (!mine || mine.status !== 'pending' || split.status !== 'pending') return;
    setBusyId(split.id);
    setError(null);
    try {
      await api.authorizeShare(split.id, mine.id);
      await load();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusyId(null);
    }
  }

  function confirmCancel(split) {
    Alert.alert(
      'Cancel split',
      'Cancel this split? Anyone who already paid will be refunded.',
      [
        { text: 'Keep', style: 'cancel' },
        {
          text: 'Cancel split',
          style: 'destructive',
          onPress: async () => {
            setBusyId(split.id);
            setError(null);
            try {
              await api.cancelSplit(split.id);
              await load();
            } catch (e) {
              setError(e.message);
            } finally {
              setBusyId(null);
            }
          },
        },
      ],
    );
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to see your splits.</Text>
        <Pressable style={styles.btn} onPress={login}><Text style={styles.btnText}>Sign in</Text></Pressable>
      </View>
    );
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <View style={styles.container}>
      {error && <Text style={styles.error}>{error}</Text>}
      <FlatList
        data={items}
        keyExtractor={(i) => i.id}
        ListEmptyComponent={<Text style={styles.meta}>No splits yet. When a friend invites you to share a booking it shows up here.</Text>}
        renderItem={({ item }) => {
          const open = !!expanded[item.id];
          const mine = item.shares.find((s) => (s.payerEmail || '').toLowerCase() === myEmail);
          const isOrganizer = item.organizerId === myUserId;
          const canAuthorise = mine && mine.status === 'pending' && item.status === 'pending';
          return (
            <View style={styles.row}>
              <Pressable onPress={() => toggle(item.id)}>
                <Text style={styles.title}>Split · {fmt(item.totalCents, item.currency)}</Text>
                <Text style={styles.meta}>
                  Status: {item.status}
                  {mine ? ` · your share ${fmt(mine.amountCents, item.currency)} (${mine.status})` : ''}
                </Text>
              </Pressable>

              {open && (
                <View style={styles.detail}>
                  {item.shares.map((s) => {
                    const isMine = (s.payerEmail || '').toLowerCase() === myEmail;
                    return (
                      <View key={s.id} style={styles.shareRow}>
                        <Text style={[styles.shareEmail, isMine && styles.shareMine]} numberOfLines={1}>
                          {s.payerEmail}{isMine ? ' (you)' : ''}
                        </Text>
                        <Text style={styles.shareAmount}>{fmt(s.amountCents, item.currency)}</Text>
                        <Text style={[
                          styles.shareStatus,
                          s.status === 'paid' ? styles.statusPaid : styles.statusPending,
                        ]}>{s.status}</Text>
                      </View>
                    );
                  })}

                  <View style={styles.actions}>
                    {canAuthorise && (
                      <Pressable
                        style={[styles.btn, busyId === item.id && styles.btnDisabled]}
                        disabled={busyId === item.id}
                        onPress={() => authorize(item)}
                      >
                        <Text style={styles.btnText}>
                          {busyId === item.id ? 'Authorising…' : `Authorise ${fmt(mine.amountCents, item.currency)}`}
                        </Text>
                      </Pressable>
                    )}
                    {isOrganizer && item.status === 'pending' && (
                      <Pressable
                        onPress={() => confirmCancel(item)}
                        disabled={busyId === item.id}
                      >
                        <Text style={styles.cancel}>Cancel split</Text>
                      </Pressable>
                    )}
                    <Pressable onPress={() => navigation.navigate('Trips')}>
                      <Text style={styles.link}>Back to trips</Text>
                    </Pressable>
                  </View>
                </View>
              )}
            </View>
          );
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, backgroundColor: '#fff' },
  row: { borderBottomWidth: 1, borderColor: '#eee', paddingVertical: 14 },
  title: { fontWeight: '700', fontSize: 16, color: '#222' },
  meta: { color: '#717171', marginTop: 4 },
  detail: { marginTop: 10, backgroundColor: '#fafafa', borderRadius: 8, padding: 12 },
  shareRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 6, borderBottomWidth: 1, borderColor: '#eee' },
  shareEmail: { flex: 1, color: '#222' },
  shareMine: { fontWeight: '700' },
  shareAmount: { width: 88, textAlign: 'right', color: '#222' },
  shareStatus: { width: 78, textAlign: 'right', textTransform: 'capitalize', fontWeight: '600' },
  statusPaid: { color: '#1e7e44' },
  statusPending: { color: '#a05a00' },
  actions: { flexDirection: 'row', alignItems: 'center', gap: 18, marginTop: 14, flexWrap: 'wrap' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 16, paddingVertical: 10 },
  btnDisabled: { opacity: 0.6 },
  btnText: { color: '#fff', fontWeight: '700' },
  cancel: { color: '#c0392b', fontWeight: '600' },
  link: { color: '#ff385c', fontWeight: '600' },
  error: { color: '#c0392b', marginBottom: 8 },
});
