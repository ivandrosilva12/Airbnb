import { useEffect, useState, useCallback } from 'react';
import { View, Text, FlatList, Image, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

export default function FavoritesScreen({ navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.listFavorites();
      setItems(res.items || []);
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

  async function remove(id) {
    setItems((prev) => prev.filter((p) => p.id !== id));
    try {
      await api.removeFavorite(id);
    } catch (e) {
      setError(e.message);
      load();
    }
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to see your saved listings.</Text>
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
        ListEmptyComponent={<Text style={styles.empty}>No saved listings yet.</Text>}
        renderItem={({ item }) => (
          <View style={styles.card}>
            <Pressable onPress={() => navigation.navigate('Property', { id: item.id })}>
              {item.photos?.[0]?.url ? (
                <Image source={{ uri: item.photos[0].url }} style={styles.photo} />
              ) : (
                <View style={[styles.photo, styles.placeholder]}><Text style={{ color: '#888' }}>No photo</Text></View>
              )}
              <View style={styles.cardBody}>
                <Text style={styles.title}>{item.title}</Text>
                <Text style={styles.meta}>{item.address.city}, {item.address.country}</Text>
                <Text style={styles.price}>{item.pricePerNight.display} / night</Text>
              </View>
            </Pressable>
            <Pressable style={styles.removeBtn} onPress={() => remove(item.id)}>
              <Text style={styles.removeText}>♥ Remove</Text>
            </Pressable>
          </View>
        )}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fafafa', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, backgroundColor: '#fff' },
  card: { backgroundColor: '#fff', borderRadius: 12, marginBottom: 16, overflow: 'hidden', borderWidth: 1, borderColor: '#eee' },
  photo: { width: '100%', height: 160 },
  placeholder: { alignItems: 'center', justifyContent: 'center', backgroundColor: '#f0f0f0' },
  cardBody: { padding: 12 },
  title: { fontWeight: '700', fontSize: 16 },
  meta: { color: '#717171', marginVertical: 2 },
  price: { fontWeight: '600' },
  removeBtn: { padding: 12, borderTopWidth: 1, borderColor: '#eee', alignItems: 'center' },
  removeText: { color: '#ff385c', fontWeight: '700' },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
});
