import { useEffect, useState, useCallback, useLayoutEffect } from 'react';
import { View, Text, FlatList, TextInput, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';

// HostCalendarScreen lets a host block date ranges on a listing (e.g. for
// maintenance or personal use) and import an external iCal feed. Blocked ranges
// behave like bookings: they make those dates unavailable to guests.
export default function HostCalendarScreen({ route, navigation }) {
  const { id, title } = route.params;
  const api = useApi();
  const [blocks, setBlocks] = useState([]);
  const [form, setForm] = useState({ from: '', to: '', reason: '' });
  const [ical, setIcal] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState(null);
  const [error, setError] = useState(null);

  useLayoutEffect(() => {
    navigation.setOptions({ title: title ? `${title} · Calendar` : 'Calendar' });
  }, [navigation, title]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.listBlocks(id);
      setBlocks(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api, id]);

  useEffect(() => {
    load();
  }, [id]);

  async function create() {
    setError(null);
    setMessage(null);
    if (!form.from || !form.to) {
      setError('Enter both dates (YYYY-MM-DD).');
      return;
    }
    setBusy(true);
    try {
      await api.createBlock(id, { from: form.from, to: form.to, reason: form.reason });
      setForm({ from: '', to: '', reason: '' });
      load();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function remove(blockId) {
    setError(null);
    try {
      await api.deleteBlock(blockId);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function doImport() {
    setError(null);
    setMessage(null);
    if (!ical.trim()) {
      setError('Paste an iCal (.ics) feed first.');
      return;
    }
    setBusy(true);
    try {
      const res = await api.importCalendar(id, ical.trim());
      setIcal('');
      setMessage(`Imported ${res.imported ?? 0} of ${res.found ?? 0} event(s).`);
      load();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <FlatList
      style={styles.container}
      data={blocks}
      keyExtractor={(b) => b.id}
      ListHeaderComponent={
        <View>
          {error && <Text style={styles.error}>{error}</Text>}
          {message && <Text style={styles.success}>{message}</Text>}

          <Text style={styles.sectionTitle}>Block dates</Text>
          <TextInput style={styles.input} placeholder="From (YYYY-MM-DD)" value={form.from} onChangeText={(v) => setForm({ ...form, from: v })} />
          <TextInput style={styles.input} placeholder="To (YYYY-MM-DD)" value={form.to} onChangeText={(v) => setForm({ ...form, to: v })} />
          <TextInput style={styles.input} placeholder="Reason (optional)" value={form.reason} onChangeText={(v) => setForm({ ...form, reason: v })} />
          <Pressable style={styles.btn} onPress={create} disabled={busy}>
            <Text style={styles.btnText}>Block these dates</Text>
          </Pressable>

          <Text style={[styles.sectionTitle, { marginTop: 20 }]}>Import iCal feed</Text>
          <TextInput
            style={[styles.input, styles.ical]}
            placeholder="Paste .ics content…"
            value={ical}
            onChangeText={setIcal}
            multiline
          />
          <Pressable style={styles.btnGhost} onPress={doImport} disabled={busy}>
            <Text style={styles.btnGhostText}>Import</Text>
          </Pressable>

          <Text style={[styles.sectionTitle, { marginTop: 20 }]}>Blocked ranges</Text>
        </View>
      }
      ListEmptyComponent={<Text style={styles.meta}>No blocked dates.</Text>}
      renderItem={({ item }) => (
        <View style={styles.row}>
          <View style={{ flex: 1 }}>
            <Text style={styles.dates}>{item.checkIn} → {item.checkOut}</Text>
            {!!item.reason && <Text style={styles.meta}>{item.reason}</Text>}
          </View>
          <Pressable onPress={() => remove(item.id)}>
            <Text style={styles.removeText}>Remove</Text>
          </Pressable>
        </View>
      )}
    />
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  sectionTitle: { fontSize: 16, fontWeight: '800', marginBottom: 8 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 10, marginBottom: 10 },
  ical: { height: 100, textAlignVertical: 'top' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 12, alignItems: 'center' },
  btnText: { color: '#fff', fontWeight: '700' },
  btnGhost: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 12, alignItems: 'center' },
  btnGhostText: { color: '#222', fontWeight: '700' },
  row: { flexDirection: 'row', alignItems: 'center', borderBottomWidth: 1, borderColor: '#eee', paddingVertical: 12 },
  dates: { fontWeight: '600' },
  meta: { color: '#717171', marginTop: 2 },
  removeText: { color: '#c0392b', fontWeight: '700' },
  success: { color: '#1a7f47', marginBottom: 8 },
  error: { color: '#c0392b', marginBottom: 8 },
});
