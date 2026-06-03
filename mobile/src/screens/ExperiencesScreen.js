import { useEffect, useState, useCallback, useRef } from 'react';
import {
  View,
  Text,
  FlatList,
  TextInput,
  Image,
  Pressable,
  StyleSheet,
  ActivityIndicator,
  ScrollView,
} from 'react-native';
import { useApi } from '../api/useApi';

const PAGE_SIZE = 12;
const CATEGORIES = ['cooking', 'tour', 'sport', 'art', 'wellness', 'other'];
const LANGUAGES = ['en', 'pt', 'es'];

// Mirrors web/src/pages/Experiences.jsx: public catalogue of published
// experiences with category/city/language filters and infinite scroll. We
// reuse ExploreScreen's load-more pattern so a host who flips between the
// two catalogues sees identical mechanics.
export default function ExperiencesScreen({ navigation }) {
  const api = useApi();
  const [category, setCategory] = useState('');
  const [city, setCity] = useState('');
  const [language, setLanguage] = useState('');
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState(null);
  const paramsRef = useRef({});

  const load = useCallback(
    async (params = {}) => {
      setLoading(true);
      setError(null);
      paramsRef.current = params;
      try {
        const cleaned = Object.fromEntries(Object.entries(params).filter(([, v]) => v));
        const res = await api.searchExperiences({ ...cleaned, limit: PAGE_SIZE, offset: 0 });
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

  const loadMore = useCallback(async () => {
    if (loading || loadingMore || items.length >= total) return;
    setLoadingMore(true);
    try {
      const cleaned = Object.fromEntries(Object.entries(paramsRef.current).filter(([, v]) => v));
      const res = await api.searchExperiences({ ...cleaned, limit: PAGE_SIZE, offset: items.length });
      setItems((prev) => [...prev, ...(res.items || [])]);
      setTotal(res.total || 0);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoadingMore(false);
    }
  }, [api, loading, loadingMore, items.length, total]);

  function applyFilters() {
    load({ category, city, language });
  }

  useEffect(() => {
    load();
  }, []);

  return (
    <View style={styles.container}>
      <View style={styles.searchRow}>
        <TextInput
          style={styles.search}
          placeholder="Search by city"
          value={city}
          onChangeText={setCity}
          onSubmitEditing={applyFilters}
          returnKeyType="search"
        />
      </View>

      {/* Category chips: tap to toggle. Tapping the active chip clears the
          filter (matches the web "any" option behaviour). */}
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.chipsRow}
        contentContainerStyle={{ gap: 8, paddingHorizontal: 4, alignItems: 'center' }}
      >
        {CATEGORIES.map((c) => {
          const on = category === c;
          return (
            <Pressable
              key={c}
              style={[styles.chip, on && styles.chipOn]}
              onPress={() => setCategory(on ? '' : c)}
            >
              <Text style={[styles.chipText, on && styles.chipTextOn]}>{c}</Text>
            </Pressable>
          );
        })}
      </ScrollView>

      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.chipsRow}
        contentContainerStyle={{ gap: 8, paddingHorizontal: 4, alignItems: 'center' }}
      >
        <Text style={styles.langLabel}>Language:</Text>
        {LANGUAGES.map((l) => {
          const on = language === l;
          return (
            <Pressable
              key={l}
              style={[styles.chip, on && styles.chipOn]}
              onPress={() => setLanguage(on ? '' : l)}
            >
              <Text style={[styles.chipText, on && styles.chipTextOn]}>{l}</Text>
            </Pressable>
          );
        })}
      </ScrollView>

      <Pressable style={styles.applyBtn} onPress={applyFilters}>
        <Text style={styles.applyText}>Apply filters</Text>
      </Pressable>

      {error && <Text style={styles.error}>{error}</Text>}

      {loading ? (
        <ActivityIndicator style={{ marginTop: 24 }} color="#ff385c" />
      ) : (
        <FlatList
          data={items}
          keyExtractor={(item) => item.id}
          onEndReached={loadMore}
          onEndReachedThreshold={0.5}
          ListEmptyComponent={<Text style={styles.empty}>No experiences found.</Text>}
          ListFooterComponent={loadingMore ? <ActivityIndicator style={{ marginVertical: 16 }} color="#ff385c" /> : null}
          renderItem={({ item }) => (
            <Pressable
              style={styles.card}
              onPress={() => navigation.navigate('ExperienceDetail', { id: item.id })}
            >
              {item.photos?.[0]?.url ? (
                <Image source={{ uri: item.photos[0].url }} style={styles.photo} />
              ) : (
                <View style={[styles.photo, styles.placeholder]}>
                  <Text style={{ color: '#888' }}>No photo</Text>
                </View>
              )}
              <View style={styles.cardBody}>
                <Text style={styles.badge}>{item.category}</Text>
                <Text style={styles.title}>{item.title}</Text>
                <Text style={styles.meta}>
                  {item.address.city}, {item.address.country}
                </Text>
                <Text style={styles.meta}>
                  {item.durationMinutes} min · {item.language}
                </Text>
                <Text style={styles.price}>{item.pricePerGuest.display} / guest</Text>
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
  searchRow: { flexDirection: 'row', gap: 8, marginBottom: 8, alignItems: 'center' },
  search: { flex: 1, backgroundColor: '#fff', borderRadius: 999, paddingHorizontal: 18, paddingVertical: 12, borderWidth: 1, borderColor: '#ddd' },
  chipsRow: { flexGrow: 0, marginBottom: 8 },
  langLabel: { color: '#717171', fontSize: 12, marginRight: 4 },
  chip: { borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 12, paddingVertical: 6, backgroundColor: '#fff' },
  chipOn: { backgroundColor: '#222', borderColor: '#222' },
  chipText: { fontSize: 13, color: '#222', textTransform: 'capitalize' },
  chipTextOn: { color: '#fff' },
  applyBtn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 12, alignItems: 'center', marginBottom: 12 },
  applyText: { color: '#fff', fontWeight: '700' },
  card: { backgroundColor: '#fff', borderRadius: 12, marginBottom: 16, overflow: 'hidden', borderWidth: 1, borderColor: '#eee' },
  photo: { width: '100%', height: 180 },
  placeholder: { alignItems: 'center', justifyContent: 'center', backgroundColor: '#f0f0f0' },
  cardBody: { padding: 12 },
  badge: { fontWeight: '700', fontSize: 12, color: '#222', marginBottom: 4, textTransform: 'capitalize' },
  title: { fontWeight: '700', fontSize: 16 },
  meta: { color: '#717171', marginVertical: 2 },
  price: { fontWeight: '600', marginTop: 4 },
  error: { color: '#c0392b', marginBottom: 8 },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
});
