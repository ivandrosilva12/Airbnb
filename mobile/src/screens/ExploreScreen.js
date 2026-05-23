import { useEffect, useState, useCallback } from 'react';
import { View, Text, FlatList, TextInput, Image, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';

export default function ExploreScreen({ navigation }) {
  const api = useApi();
  const [city, setCity] = useState('');
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const load = useCallback(
    async (params = {}) => {
      setLoading(true);
      setError(null);
      try {
        const cleaned = Object.fromEntries(Object.entries(params).filter(([, v]) => v));
        const res = await api.searchProperties(cleaned);
        setItems(res.items || []);
      } catch (e) {
        setError(e.message);
      } finally {
        setLoading(false);
      }
    },
    [api],
  );

  useEffect(() => {
    load();
  }, []);

  return (
    <View style={styles.container}>
      <TextInput
        style={styles.search}
        placeholder="Search by city"
        value={city}
        onChangeText={setCity}
        onSubmitEditing={() => load({ city })}
        returnKeyType="search"
      />
      {error && <Text style={styles.error}>{error}</Text>}
      {loading ? (
        <ActivityIndicator style={{ marginTop: 24 }} color="#ff385c" />
      ) : (
        <FlatList
          data={items}
          keyExtractor={(item) => item.id}
          ListEmptyComponent={<Text style={styles.empty}>No listings found.</Text>}
          renderItem={({ item }) => (
            <Pressable style={styles.card} onPress={() => navigation.navigate('Property', { id: item.id })}>
              {item.photos?.[0]?.url ? (
                <Image source={{ uri: item.photos[0].url }} style={styles.photo} />
              ) : (
                <View style={[styles.photo, styles.placeholder]}>
                  <Text style={{ color: '#888' }}>No photo</Text>
                </View>
              )}
              <View style={styles.cardBody}>
                <Text style={styles.title}>{item.title}</Text>
                <Text style={styles.meta}>{item.address.city}, {item.address.country}</Text>
                <Text style={styles.price}>{item.pricePerNight.display} / night</Text>
              </View>
            </Pressable>
          )}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fafafa', padding: 12 },
  search: { backgroundColor: '#fff', borderRadius: 999, paddingHorizontal: 18, paddingVertical: 12, borderWidth: 1, borderColor: '#ddd', marginBottom: 12 },
  card: { backgroundColor: '#fff', borderRadius: 12, marginBottom: 16, overflow: 'hidden', borderWidth: 1, borderColor: '#eee' },
  photo: { width: '100%', height: 180 },
  placeholder: { alignItems: 'center', justifyContent: 'center', backgroundColor: '#f0f0f0' },
  cardBody: { padding: 12 },
  title: { fontWeight: '700', fontSize: 16 },
  meta: { color: '#717171', marginVertical: 2 },
  price: { fontWeight: '600' },
  error: { color: '#c0392b', marginBottom: 8 },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
});
