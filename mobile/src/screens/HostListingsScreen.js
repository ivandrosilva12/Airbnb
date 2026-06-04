import { useEffect, useState, useCallback } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator, Alert } from 'react-native';
import { useApi } from '../api/useApi';

// TODO(i18n): translate — wrap hardcoded labels via useT() from ../i18n/I18nContext.
// Key namespace mirrors the web (host.*, host.metric.*).

export default function HostListingsScreen({ navigation }) {
  const api = useApi();
  const [listings, setListings] = useState([]);
  const [metrics, setMetrics] = useState(null);
  const [earnings, setEarnings] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  // Track which row is currently mid-duplicate so the user can't double-tap
  // and clone the same listing twice. Scoped per id so other rows stay live.
  const [duplicating, setDuplicating] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [props, m, e] = await Promise.all([
        api.myProperties(),
        api.hostMetrics().catch(() => null),
        api.hostEarnings().catch(() => null),
      ]);
      setListings(props.items || []);
      setMetrics(m);
      setEarnings((e?.balances || [])[0] || null);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    const unsub = navigation.addListener('focus', load);
    return unsub;
  }, [navigation, load]);

  // confirmDuplicate fires the iOS/Android native alert so accidental taps
  // can be cancelled. The backend returns the new draft id; we navigate
  // straight into the edit form (mirrors the web UX from S60).
  function confirmDuplicate(item) {
    Alert.alert(
      'Duplicate listing',
      `Create a copy of "${item.title}" as a draft? Photos and arrival info won't carry over.`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Duplicate',
          onPress: async () => {
            setDuplicating(item.id);
            setError(null);
            try {
              const res = await api.duplicateProperty(item.id);
              await load();
              if (res && res.id) {
                navigation.navigate('HostListingForm', { id: res.id });
              }
            } catch (e) {
              setError(e.message);
            } finally {
              setDuplicating(null);
            }
          },
        },
      ],
    );
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <View style={styles.container}>
      {error && <Text style={styles.error}>{error}</Text>}
      {metrics && (
        <View style={styles.metrics}>
          <Metric value={earnings ? earnings.net.display : metrics.capturedRevenue.display} label={earnings ? 'Earnings (net)' : 'Revenue'} />
          <Metric value={String(metrics.confirmed)} label="Confirmed" />
          <Metric value={String(metrics.upcomingCheckins)} label="Upcoming" />
        </View>
      )}
      <Pressable style={styles.newBtn} onPress={() => navigation.navigate('HostListingForm')}>
        <Text style={styles.newBtnText}>+ New listing</Text>
      </Pressable>
      <FlatList
        data={listings}
        keyExtractor={(i) => i.id}
        ListEmptyComponent={<Text style={styles.empty}>No listings yet.</Text>}
        renderItem={({ item }) => (
          <View style={styles.row}>
            <Pressable
              style={{ flex: 1 }}
              onPress={() => navigation.navigate('HostPropertyBookings', { id: item.id, title: item.title })}
            >
              <Text style={styles.title}>{item.title}</Text>
              <Text style={styles.meta}>{item.address.city} · {item.pricePerNight.display}/night</Text>
            </Pressable>
            <View style={[styles.badge, item.status === 'published' ? styles.badgeOn : styles.badgeOff]}>
              <Text style={styles.badgeText}>{item.status}</Text>
            </View>
            <Pressable style={styles.editBtn} onPress={() => navigation.navigate('HostListingForm', { id: item.id })}>
              <Text style={styles.editText}>Edit</Text>
            </Pressable>
            <Pressable
              style={styles.dupBtn}
              onPress={() => confirmDuplicate(item)}
              disabled={duplicating === item.id}
              accessibilityRole="button"
              accessibilityLabel={`Duplicate ${item.title}`}
            >
              <Text style={styles.dupText}>
                {duplicating === item.id ? '…' : 'Duplicate'}
              </Text>
            </Pressable>
          </View>
        )}
      />
    </View>
  );
}

function Metric({ value, label }) {
  return (
    <View style={styles.metric}>
      <Text style={styles.metricValue}>{value}</Text>
      <Text style={styles.metricLabel}>{label}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  metrics: { flexDirection: 'row', gap: 10, marginBottom: 16 },
  metric: { flex: 1, borderWidth: 1, borderColor: '#eee', borderRadius: 10, padding: 12, alignItems: 'center' },
  metricValue: { fontWeight: '800', fontSize: 16 },
  metricLabel: { color: '#717171', fontSize: 12, marginTop: 2, textAlign: 'center' },
  newBtn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 12, alignItems: 'center', marginBottom: 12 },
  newBtnText: { color: '#fff', fontWeight: '700' },
  editBtn: { marginLeft: 10, paddingHorizontal: 10, paddingVertical: 6 },
  editText: { color: '#ff385c', fontWeight: '700' },
  dupBtn: { marginLeft: 6, paddingHorizontal: 10, paddingVertical: 6 },
  dupText: { color: '#717171', fontWeight: '700' },
  row: { flexDirection: 'row', alignItems: 'center', paddingVertical: 14, borderBottomWidth: 1, borderColor: '#eee' },
  title: { fontWeight: '600', fontSize: 16 },
  meta: { color: '#717171', marginTop: 2 },
  badge: { borderRadius: 999, paddingHorizontal: 10, paddingVertical: 4 },
  badgeOn: { backgroundColor: '#e6f7ed' },
  badgeOff: { backgroundColor: '#fff5e6' },
  badgeText: { fontSize: 12, fontWeight: '700', color: '#444' },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
});
