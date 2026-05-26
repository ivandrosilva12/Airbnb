import { useEffect, useState, useCallback, useRef } from 'react';
import { View, Text, FlatList, TextInput, Image, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';

const PAGE_SIZE = 12;

export default function ExploreScreen({ navigation }) {
  const api = useApi();
  const [city, setCity] = useState('');
  const [minPrice, setMinPrice] = useState('');
  const [maxPrice, setMaxPrice] = useState('');
  const [bedrooms, setBedrooms] = useState('');
  const [instantOnly, setInstantOnly] = useState(false);
  const [showFilters, setShowFilters] = useState(false);
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState(null);
  const paramsRef = useRef({});

  function applyFilters() {
    const toCents = (v) => (v ? Math.round(Number(v) * 100) : '');
    load({
      city,
      minPrice: toCents(minPrice),
      maxPrice: toCents(maxPrice),
      bedrooms: bedrooms || '',
      ...(instantOnly ? { instantBook: 'true' } : {}),
    });
  }

  const load = useCallback(
    async (params = {}) => {
      setLoading(true);
      setError(null);
      paramsRef.current = params;
      try {
        const cleaned = Object.fromEntries(Object.entries(params).filter(([, v]) => v));
        const res = await api.searchProperties({ ...cleaned, limit: PAGE_SIZE, offset: 0 });
        setItems(res.items || []);
        setTotal(res.total || 0);
      } catch (e) {
        setError(e.message);
      } finally {
        setLoading(false);
      }
    },
    [api],
  );

  // loadMore appends the next page when the user scrolls to the end, until every
  // result is loaded. Guards against overlapping fetches and the initial load.
  const loadMore = useCallback(async () => {
    if (loading || loadingMore || items.length >= total) return;
    setLoadingMore(true);
    try {
      const cleaned = Object.fromEntries(Object.entries(paramsRef.current).filter(([, v]) => v));
      const res = await api.searchProperties({ ...cleaned, limit: PAGE_SIZE, offset: items.length });
      setItems((prev) => [...prev, ...(res.items || [])]);
      setTotal(res.total || 0);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoadingMore(false);
    }
  }, [api, loading, loadingMore, items.length, total]);

  useEffect(() => {
    load();
  }, []);

  return (
    <View style={styles.container}>
      <View style={styles.searchRow}>
        <TextInput
          style={[styles.search, { flex: 1, marginBottom: 0 }]}
          placeholder="Search by city"
          value={city}
          onChangeText={setCity}
          onSubmitEditing={applyFilters}
          returnKeyType="search"
        />
        <Pressable style={styles.filterToggle} onPress={() => setShowFilters((v) => !v)}>
          <Text style={styles.filterToggleText}>Filters</Text>
        </Pressable>
      </View>
      {showFilters && (
        <View style={styles.filters}>
          <View style={styles.filterRow}>
            <TextInput style={styles.filterInput} placeholder="Min price" keyboardType="number-pad" value={minPrice} onChangeText={setMinPrice} />
            <TextInput style={styles.filterInput} placeholder="Max price" keyboardType="number-pad" value={maxPrice} onChangeText={setMaxPrice} />
            <TextInput style={styles.filterInput} placeholder="Bedrooms" keyboardType="number-pad" value={bedrooms} onChangeText={setBedrooms} />
          </View>
          <Pressable style={styles.instantToggle} onPress={() => setInstantOnly((v) => !v)}>
            <Text style={styles.instantBox}>{instantOnly ? '☑' : '☐'}</Text>
            <Text style={styles.instantLabel}>Instant Book only</Text>
          </Pressable>
          <Pressable style={styles.applyBtn} onPress={applyFilters}>
            <Text style={styles.applyText}>Apply filters</Text>
          </Pressable>
        </View>
      )}
      {error && <Text style={styles.error}>{error}</Text>}
      {loading ? (
        <ActivityIndicator style={{ marginTop: 24 }} color="#ff385c" />
      ) : (
        <FlatList
          data={items}
          keyExtractor={(item) => item.id}
          onEndReached={loadMore}
          onEndReachedThreshold={0.5}
          ListEmptyComponent={<Text style={styles.empty}>No listings found.</Text>}
          ListFooterComponent={loadingMore ? <ActivityIndicator style={{ marginVertical: 16 }} color="#ff385c" /> : null}
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
                {item.hostIsSuperhost && <Text style={styles.superhost}>★ Superhost</Text>}
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
  searchRow: { flexDirection: 'row', gap: 8, marginBottom: 12, alignItems: 'center' },
  filterToggle: { borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 16, paddingVertical: 12, backgroundColor: '#fff' },
  filterToggleText: { fontWeight: '700', color: '#222' },
  filters: { backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#eee', padding: 12, marginBottom: 12 },
  filterRow: { flexDirection: 'row', gap: 8 },
  filterInput: { flex: 1, borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 8 },
  instantToggle: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 10 },
  instantBox: { fontSize: 18 },
  instantLabel: { color: '#222' },
  applyBtn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 12, alignItems: 'center', marginTop: 10 },
  applyText: { color: '#fff', fontWeight: '700' },
  card: { backgroundColor: '#fff', borderRadius: 12, marginBottom: 16, overflow: 'hidden', borderWidth: 1, borderColor: '#eee' },
  photo: { width: '100%', height: 180 },
  placeholder: { alignItems: 'center', justifyContent: 'center', backgroundColor: '#f0f0f0' },
  cardBody: { padding: 12 },
  superhost: { fontWeight: '700', fontSize: 12, color: '#222', marginBottom: 2 },
  title: { fontWeight: '700', fontSize: 16 },
  meta: { color: '#717171', marginVertical: 2 },
  price: { fontWeight: '600' },
  error: { color: '#c0392b', marginBottom: 8 },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
});
