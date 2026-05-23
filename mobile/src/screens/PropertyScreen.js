import { useEffect, useState } from 'react';
import { View, Text, Image, ScrollView, TextInput, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

export default function PropertyScreen({ route }) {
  const { id } = route.params;
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [property, setProperty] = useState(null);
  const [checkIn, setCheckIn] = useState('');
  const [checkOut, setCheckOut] = useState('');
  const [guests, setGuests] = useState('1');
  const [message, setMessage] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.getProperty(id).then(setProperty).catch((e) => setError(e.message));
  }, [id]);

  async function book() {
    setError(null);
    setMessage(null);
    if (!authenticated) {
      login();
      return;
    }
    try {
      const b = await api.createBooking({ propertyId: id, checkIn, checkOut, guests: Number(guests) });
      setMessage(`Booked ${b.nights} night(s) for ${b.totalPrice.display}. Status: ${b.status}.`);
    } catch (e) {
      setError(e.message);
    }
  }

  if (!property) {
    return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;
  }

  return (
    <ScrollView style={styles.container}>
      {property.photos?.[0]?.url && <Image source={{ uri: property.photos[0].url }} style={styles.hero} />}
      <View style={styles.body}>
        <Text style={styles.title}>{property.title}</Text>
        <Text style={styles.meta}>{property.address.city}, {property.address.country} · up to {property.maxGuests} guests</Text>
        <Text style={styles.price}>{property.pricePerNight.display} / night</Text>
        <Text style={styles.desc}>{property.description || 'No description provided.'}</Text>

        <View style={styles.bookBox}>
          <Text style={styles.bookTitle}>Reserve</Text>
          <TextInput style={styles.input} placeholder="Check in (YYYY-MM-DD)" value={checkIn} onChangeText={setCheckIn} />
          <TextInput style={styles.input} placeholder="Check out (YYYY-MM-DD)" value={checkOut} onChangeText={setCheckOut} />
          <TextInput style={styles.input} placeholder="Guests" keyboardType="number-pad" value={guests} onChangeText={setGuests} />
          <Pressable style={styles.btn} onPress={book}>
            <Text style={styles.btnText}>{authenticated ? 'Reserve' : 'Sign in to reserve'}</Text>
          </Pressable>
          {message && <Text style={styles.success}>{message}</Text>}
          {error && <Text style={styles.error}>{error}</Text>}
        </View>
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  hero: { width: '100%', height: 240 },
  body: { padding: 16 },
  title: { fontSize: 22, fontWeight: '800' },
  meta: { color: '#717171', marginVertical: 4 },
  price: { fontSize: 16, fontWeight: '600', marginVertical: 4 },
  desc: { marginVertical: 12, lineHeight: 20 },
  bookBox: { borderWidth: 1, borderColor: '#ddd', borderRadius: 12, padding: 16, marginTop: 8 },
  bookTitle: { fontWeight: '700', fontSize: 16, marginBottom: 10 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 10, marginBottom: 10 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 12, alignItems: 'center' },
  btnText: { color: '#fff', fontWeight: '700' },
  success: { color: '#1a7f47', marginTop: 10 },
  error: { color: '#c0392b', marginTop: 10 },
});
