import { useCallback, useEffect, useState } from 'react';
import { View, Text, TextInput, FlatList, Pressable, StyleSheet, ActivityIndicator, Alert } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

const MAX_LABEL = 60;
const MAX_BODY = 4000;

// SavedRepliesScreen is the mobile-side editor for the host's reply playbook.
// Same data set the messaging composer pulls; CRUD here, chips there. We keep
// the form inline at the top instead of a modal so a host can quickly add a
// one-liner while reading a thread on the next tab.
export default function SavedRepliesScreen() {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [items, setItems] = useState([]);
  const [draft, setDraft] = useState({ label: '', body: '' });
  const [editingId, setEditingId] = useState(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const reload = useCallback(async () => {
    setError(null);
    try {
      const r = await api.listMessageTemplates();
      setItems(r.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    if (authenticated) reload();
    else setLoading(false);
  }, [authenticated, reload]);

  async function save() {
    if (!draft.label.trim() || !draft.body.trim()) {
      setError('Label and body are required.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      if (editingId) {
        await api.updateMessageTemplate(editingId, draft);
      } else {
        await api.createMessageTemplate(draft);
      }
      setDraft({ label: '', body: '' });
      setEditingId(null);
      await reload();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  function startEdit(it) {
    setEditingId(it.id);
    setDraft({ label: it.label, body: it.body });
  }

  function cancelEdit() {
    setEditingId(null);
    setDraft({ label: '', body: '' });
  }

  function confirmRemove(id) {
    Alert.alert(
      'Delete saved reply',
      'Remove this template? Threads that already used it keep the text.',
      [
        { text: 'Keep', style: 'cancel' },
        {
          text: 'Delete',
          style: 'destructive',
          onPress: async () => {
            setError(null);
            try {
              await api.deleteMessageTemplate(id);
              if (editingId === id) cancelEdit();
              await reload();
            } catch (e) {
              setError(e.message);
            }
          },
        },
      ],
    );
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to manage your saved replies.</Text>
        <Pressable style={styles.btn} onPress={login}><Text style={styles.btnText}>Sign in</Text></Pressable>
      </View>
    );
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <View style={styles.container}>
      <Text style={styles.hint}>
        Your playbook — short, reusable replies the messaging composer turns into chips.
      </Text>
      {error && <Text style={styles.error}>{error}</Text>}

      <View style={styles.form}>
        <Text style={styles.formLabel}>{editingId ? 'Edit reply' : 'Add a reply'}</Text>
        <TextInput
          style={styles.input}
          placeholder="Label (e.g. Wi-Fi password)"
          placeholderTextColor="#999"
          maxLength={MAX_LABEL}
          value={draft.label}
          onChangeText={(v) => setDraft((d) => ({ ...d, label: v }))}
        />
        <TextInput
          style={[styles.input, { minHeight: 90 }]}
          placeholder="Body — the text that will be inserted"
          placeholderTextColor="#999"
          maxLength={MAX_BODY}
          multiline
          value={draft.body}
          onChangeText={(v) => setDraft((d) => ({ ...d, body: v }))}
        />
        <View style={styles.formActions}>
          <Pressable style={[styles.btn, busy && styles.btnDisabled]} disabled={busy} onPress={save}>
            <Text style={styles.btnText}>{busy ? 'Saving…' : editingId ? 'Save' : 'Add'}</Text>
          </Pressable>
          {editingId && (
            <Pressable onPress={cancelEdit}>
              <Text style={styles.cancelText}>Cancel</Text>
            </Pressable>
          )}
        </View>
      </View>

      <FlatList
        data={items}
        keyExtractor={(it) => it.id}
        ListEmptyComponent={<Text style={styles.meta}>No saved replies yet.</Text>}
        renderItem={({ item }) => (
          <View style={[styles.item, editingId === item.id && styles.itemEditing]}>
            <Text style={styles.itemLabel}>{item.label}</Text>
            <Text style={styles.itemBody} numberOfLines={3}>{item.body}</Text>
            <View style={styles.itemActions}>
              <Pressable onPress={() => startEdit(item)}>
                <Text style={styles.editText}>Edit</Text>
              </Pressable>
              <Pressable onPress={() => confirmRemove(item.id)}>
                <Text style={styles.deleteText}>Delete</Text>
              </Pressable>
            </View>
          </View>
        )}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, backgroundColor: '#fff' },
  hint: { color: '#717171', marginBottom: 12 },
  form: { backgroundColor: '#fafafa', borderRadius: 8, padding: 12, marginBottom: 14 },
  formLabel: { fontWeight: '700', color: '#222', marginBottom: 6 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, paddingHorizontal: 10, paddingVertical: 8, marginTop: 6, backgroundColor: '#fff' },
  formActions: { flexDirection: 'row', alignItems: 'center', gap: 18, marginTop: 10 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 16, paddingVertical: 10 },
  btnDisabled: { opacity: 0.6 },
  btnText: { color: '#fff', fontWeight: '700' },
  cancelText: { color: '#717171', fontWeight: '600' },
  item: { borderBottomWidth: 1, borderColor: '#eee', paddingVertical: 10 },
  itemEditing: { backgroundColor: '#fff7f9' },
  itemLabel: { fontWeight: '700', color: '#222', fontSize: 15 },
  itemBody: { color: '#717171', marginTop: 4 },
  itemActions: { flexDirection: 'row', alignItems: 'center', gap: 16, marginTop: 8 },
  editText: { color: '#ff385c', fontWeight: '600' },
  deleteText: { color: '#c0392b', fontWeight: '600' },
  meta: { color: '#717171', textAlign: 'center', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
});
