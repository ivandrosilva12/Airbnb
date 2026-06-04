import { useEffect, useState, useCallback } from 'react';
import { View, Text, FlatList, Image, Pressable, TextInput, StyleSheet, ActivityIndicator, ScrollView, Alert, Share } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { WEB_BASE_URL } from '../config';

// TODO(i18n): translate — wrap hardcoded labels via useT() from ../i18n/I18nContext.
// Key namespace mirrors the web (fav.*).

export default function FavoritesScreen({ navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [collections, setCollections] = useState([]);
  const [filter, setFilter] = useState('all'); // 'all' | 'none' | <collectionId>
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [newName, setNewName] = useState('');
  const [movingId, setMovingId] = useState(null);

  const loadCollections = useCallback(async () => {
    try {
      const res = await api.listCollections();
      setCollections(res.items || []);
    } catch {
      /* non-fatal */
    }
  }, [api]);

  const loadItems = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.listFavorites(filter === 'all' ? undefined : filter);
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api, filter]);

  useEffect(() => {
    if (!authenticated) {
      setLoading(false);
      return;
    }
    loadCollections();
    loadItems();
  }, [authenticated, loadCollections, loadItems]);

  async function remove(id) {
    setItems((prev) => prev.filter((p) => p.id !== id));
    try {
      await api.removeFavorite(id);
      loadCollections();
    } catch (e) {
      setError(e.message);
      loadItems();
    }
  }

  async function createCollection() {
    const name = newName.trim();
    if (!name) return;
    try {
      await api.createCollection(name);
      setNewName('');
      loadCollections();
    } catch (e) {
      setError(e.message);
    }
  }

  function confirmDeleteCollection(id) {
    Alert.alert('Delete collection', 'Saved listings move back to Unsorted.', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete',
        style: 'destructive',
        onPress: async () => {
          try {
            await api.deleteCollection(id);
            if (filter === id) setFilter('all');
            loadCollections();
            loadItems();
          } catch (e) {
            setError(e.message);
          }
        },
      },
    ]);
  }

  async function move(propertyId, collectionId) {
    setMovingId(null);
    try {
      await api.moveFavorite(propertyId, collectionId);
      loadCollections();
      loadItems();
    } catch (e) {
      setError(e.message);
    }
  }

  // Turn on a public link for the selected collection and open the share sheet.
  async function shareCurrent(collection) {
    try {
      const token = collection.shareToken || (await api.shareCollection(collection.id)).shareToken;
      await loadCollections();
      await Share.share({ message: `${collection.name} — ${WEB_BASE_URL}/wishlist/${token}` });
    } catch (e) {
      setError(e.message);
    }
  }

  function confirmUnshare(id) {
    Alert.alert('Stop sharing', 'The current link will stop working.', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Stop sharing',
        style: 'destructive',
        onPress: async () => {
          try {
            await api.unshareCollection(id);
            loadCollections();
          } catch (e) {
            setError(e.message);
          }
        },
      },
    ]);
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to see your saved listings.</Text>
        <Pressable style={styles.btn} onPress={login}><Text style={styles.btnText}>Sign in</Text></Pressable>
      </View>
    );
  }

  const chips = [
    { key: 'all', label: 'All' },
    { key: 'none', label: 'Unsorted' },
    ...collections.map((c) => ({ key: c.id, label: `${c.name} (${c.count})`, deletable: true })),
  ];

  return (
    <View style={styles.container}>
      {error && <Text style={styles.error}>{error}</Text>}

      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.chipRow} contentContainerStyle={{ gap: 8, paddingVertical: 4 }}>
        {chips.map((c) => (
          <Pressable
            key={c.key}
            style={[styles.chip, filter === c.key && styles.chipOn]}
            onPress={() => setFilter(c.key)}
            onLongPress={() => c.deletable && confirmDeleteCollection(c.key)}
          >
            <Text style={[styles.chipText, filter === c.key && styles.chipTextOn]}>{c.label}</Text>
          </Pressable>
        ))}
      </ScrollView>

      <View style={styles.newRow}>
        <TextInput
          style={styles.newInput}
          placeholder="New collection"
          value={newName}
          onChangeText={setNewName}
          maxLength={60}
          onSubmitEditing={createCollection}
          returnKeyType="done"
        />
        <Pressable style={styles.newBtn} onPress={createCollection}><Text style={styles.newBtnText}>Add</Text></Pressable>
      </View>

      {(() => {
        const selected = collections.find((c) => c.id === filter);
        if (!selected) return null;
        return (
          <View style={styles.shareRow}>
            <Pressable onPress={() => shareCurrent(selected)}>
              <Text style={styles.shareText}>🔗 {selected.shareToken ? 'Share link' : 'Share this collection'}</Text>
            </Pressable>
            {selected.shareToken && (
              <Pressable onPress={() => confirmUnshare(selected.id)}>
                <Text style={styles.unshareText}>Stop sharing</Text>
              </Pressable>
            )}
          </View>
        );
      })()}

      {loading ? (
        <ActivityIndicator style={{ marginTop: 24 }} color="#ff385c" />
      ) : (
        <FlatList
          data={items}
          keyExtractor={(i) => i.id}
          ListEmptyComponent={<Text style={styles.empty}>No saved listings here yet.</Text>}
          renderItem={({ item }) => (
            <View style={styles.card}>
              <Pressable onPress={() => navigation.navigate('Property', { id: item.id })}>
                {item.photos?.[0]?.url ? (
                  <Image source={{ uri: item.photos[0].url }} style={styles.photo} />
                ) : (
                  <View style={[styles.photo, styles.placeholder]}><Text style={{ color: '#888' }}>No photo</Text></View>
                )}
                <View style={styles.cardBody}>
                  {item.hostIsSuperhost && <Text style={styles.superhost}>★ Superhost</Text>}
                  <Text style={styles.title}>{item.title}</Text>
                  <Text style={styles.meta}>{item.address.city}, {item.address.country}</Text>
                  <Text style={styles.price}>{item.pricePerNight.display} / night</Text>
                </View>
              </Pressable>

              {movingId === item.id ? (
                <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.moveRow} contentContainerStyle={{ gap: 8, padding: 10 }}>
                  <Pressable style={styles.moveChip} onPress={() => move(item.id, '')}><Text style={styles.moveChipText}>Unsorted</Text></Pressable>
                  {collections.map((c) => (
                    <Pressable key={c.id} style={styles.moveChip} onPress={() => move(item.id, c.id)}>
                      <Text style={styles.moveChipText}>{c.name}</Text>
                    </Pressable>
                  ))}
                </ScrollView>
              ) : (
                <View style={styles.actions}>
                  <Pressable onPress={() => setMovingId(item.id)}><Text style={styles.moveText}>📁 Move</Text></Pressable>
                  <Pressable onPress={() => remove(item.id)}><Text style={styles.removeText}>♥ Remove</Text></Pressable>
                </View>
              )}
            </View>
          )}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fafafa', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, backgroundColor: '#fff' },
  chipRow: { flexGrow: 0, marginBottom: 8 },
  chip: { borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 14, paddingVertical: 7, backgroundColor: '#fff' },
  chipOn: { backgroundColor: '#ff385c', borderColor: '#ff385c' },
  chipText: { fontSize: 13, color: '#222' },
  chipTextOn: { color: '#fff', fontWeight: '700' },
  newRow: { flexDirection: 'row', gap: 8, marginBottom: 12 },
  newInput: { flex: 1, backgroundColor: '#fff', borderRadius: 8, borderWidth: 1, borderColor: '#ddd', paddingHorizontal: 12, paddingVertical: 8 },
  newBtn: { backgroundColor: '#222', borderRadius: 8, paddingHorizontal: 16, justifyContent: 'center' },
  newBtnText: { color: '#fff', fontWeight: '700' },
  shareRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 },
  shareText: { color: '#ff385c', fontWeight: '700' },
  unshareText: { color: '#c0392b', textDecorationLine: 'underline' },
  card: { backgroundColor: '#fff', borderRadius: 12, marginBottom: 16, overflow: 'hidden', borderWidth: 1, borderColor: '#eee' },
  photo: { width: '100%', height: 160 },
  placeholder: { alignItems: 'center', justifyContent: 'center', backgroundColor: '#f0f0f0' },
  cardBody: { padding: 12 },
  superhost: { fontWeight: '700', fontSize: 12, color: '#222', marginBottom: 2 },
  title: { fontWeight: '700', fontSize: 16 },
  meta: { color: '#717171', marginVertical: 2 },
  price: { fontWeight: '600' },
  actions: { flexDirection: 'row', justifyContent: 'space-between', padding: 12, borderTopWidth: 1, borderColor: '#eee' },
  moveText: { color: '#222', fontWeight: '700' },
  moveRow: { borderTopWidth: 1, borderColor: '#eee' },
  moveChip: { borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 12, paddingVertical: 6, backgroundColor: '#fff' },
  moveChipText: { fontSize: 13, color: '#222' },
  removeText: { color: '#ff385c', fontWeight: '700' },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
});
